package inventory

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// Nhập số đếm kiểm kho bằng Excel (kế hoạch M7, Task 263 = M7-18). Ba bước:
//  1. PrepareCountImport (handler, **ngoài** transaction, ADR-46): đọc tệp mục đích inventory_count_import (files.ReadContent,
//     tối đa 5 MiB), băm SHA-256 nội dung (khóa chống nhập trùng: đổi tên tệp vẫn cùng băm), đọc trang đầu bằng
//     xlsx.Read với cột "Mã" (SKU hoặc barcode) và "Số đếm"; dòng hỏng (không phải số nguyên, âm, ô công thức) thành lỗi
//     theo số dòng Excel, dòng còn lại giữ (số dòng, mã, số).
//  2. CreateCountImport (trong WriteTx): khóa chia sẻ phiên mở sổ còn mở, ghi bản nhập queued và việc nền
//     inventory.count_import (DedupeKey = mã bản nhập). Cùng tệp, cùng phiên, cùng mode đã có: replace → trả bản cũ
//     (nhập lại cùng tệp không đổi gì), add → ErrImportAlreadyApplied (cộng lần hai sẽ đếm đôi).
//  3. Việc nền (transaction của hàng đợi, không đọc kho tệp): khóa chia sẻ phiên (đã đăng hay hủy → failed, không đổi dòng
//     nào), tra mã (lạ → unknown_code, trùng giữa người bán → ambiguous_code), cộng các dòng cùng biến thể, upsert dòng
//     đếm theo variant_id tăng dần trong một câu (replace: đặt bằng tổng; add: cộng), rồi đặt done kèm số dòng đã áp và
//     lỗi từng dòng. Lỗi chỉ ghi số dòng và mã lỗi, không ghi nội dung ô.
//
// Thứ tự khóa (ADR-46 nhóm 11): stocktakes (FOR SHARE) → stocktake_lines → count_imports.

// CountImportPurposeKey là mục đích tệp của tệp Excel số đếm (dòng của platform.file_purposes, migration 0705).
const CountImportPurposeKey = "inventory_count_import"

// Giới hạn của một bản nhập số đếm.
const (
	// MaxCountImportBytes là dung lượng tối đa của tệp (5 MiB).
	MaxCountImportBytes int64 = 5 << 20
	// MaxCountImportRows là số dòng dữ liệu tối đa của tệp.
	MaxCountImportRows = 20_000
)

// Chế độ của bản nhập (CHECK của inventory.count_imports.mode).
const (
	CountImportReplace = "replace"
	CountImportAdd     = "add"
)

// Trạng thái của bản nhập (CHECK của inventory.count_imports.status; running dành cho chạy theo lô sau này, hiện việc
// nền đi thẳng queued → done/failed trong một transaction).
const (
	CountImportQueued  = "queued"
	CountImportRunning = "running"
	CountImportDone    = "done"
	CountImportFailed  = "failed"
)

// Mã lỗi một dòng của tệp số đếm (enum CountImportRowError.code của openapi).
const (
	rowUnknownCode   = "unknown_code"
	rowAmbiguousCode = "ambiguous_code"
	rowNotInteger    = "not_integer"
	rowNegative      = "negative"
	rowFormula       = "formula"
)

const (
	colCode  = "Mã"
	colCount = "Số đếm"

	countImportKindName = "inventory.count_import"
	countImportAttempts = 8
	countImportTimeout  = 30 * time.Second
	xlsxContentType     = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

// errImportFinished: bản nhập đã kết thúc ở lượt khác giữa lúc lượt này đọc và ghi; transaction lùi, lượt sau thấy xong.
var errImportFinished = errors.New("inventory: bản nhập số đếm đã kết thúc")

// CountImportHeader là dòng tiêu đề của tệp số đếm, theo thứ tự cột.
func CountImportHeader() []string { return []string{colCode, colCount} }

// CountImportPurpose là mục đích tệp inventory_count_import: bucket private, chỉ .xlsx, tối đa 5 MiB, nhân viên tải
// lên được.
func CountImportPurpose() files.Purpose {
	p := files.Purpose{Key: CountImportPurposeKey, Bucket: files.BucketPrivate, MaxBytes: MaxCountImportBytes, Upload: true}
	p.AllowedTypes = []string{xlsxContentType}
	return p
}

// RegisterCountImportPurpose đăng ký mục đích tệp inventory_count_import vào sổ của tiến trình (cmd/crm gọi một lần lúc
// khởi động, trước files.CheckPurposes). Gọi lại nhiều lần không sao.
func RegisterCountImportPurpose() { files.RegisterPurpose(CountImportPurpose()) }

// CountImportRowError là lỗi một dòng của tệp: số dòng Excel (tiêu đề là dòng 1) và mã lỗi; không nội dung ô.
type CountImportRowError struct {
	Row  int    `json:"row"`
	Code string `json:"code"`
}

// CountImport là một bản nhập số đếm.
type CountImport struct {
	ID, StocktakeID, FileID, CreatedBy uuid.UUID
	Mode, Status                       string
	AppliedRows                        int
	ErrorRows                          []CountImportRowError
	CreatedAt                          time.Time
	FinishedAt                         *time.Time
}

// pendingRow là một dòng số đọc được, chờ việc nền tra mã: số dòng Excel, mã quét (SKU/barcode), số đếm.
type pendingRow struct {
	Row  int    `json:"row"`
	Code string `json:"code"`
	Qty  int32  `json:"qty"`
}

// CountImportFile là tệp số đếm đã đọc, băm và phân tích (PrepareCountImport), dùng cho CreateCountImport của cùng tenant.
type CountImportFile struct {
	tenantID, fileID uuid.UUID
	sha256           string
	rows             []pendingRow
	errs             []CountImportRowError
}

// PrepareCountImport đọc tệp fileID của tenantID **ngoài** transaction, băm và phân tích. Lỗi: ErrNotFound (tệp không
// có, của tenant khác, đã xóa), files.ErrTooLarge (quá 5 MiB), ErrInvalidInput (chưa tải xong, sai mục đích, mất trên kho,
// không phải .xlsx, sai tiêu đề "Mã", "Số đếm", quá 20 000 dòng); chuỗi lỗi không chứa nội dung ô.
func PrepareCountImport(ctx context.Context, d *db.DB, fs files.FileStore, tenantID, fileID uuid.UUID) (CountImportFile, error) {
	body, err := files.ReadContent(ctx, d, fs, tenantID, fileID, CountImportPurposeKey, MaxCountImportBytes)
	switch {
	case errors.Is(err, files.ErrNotFound):
		return CountImportFile{}, ErrNotFound
	case errors.Is(err, files.ErrNotReady), errors.Is(err, files.ErrObjectNotFound):
		return CountImportFile{}, fmt.Errorf("%w: tệp số đếm chưa tải xong hoặc sai mục đích", ErrInvalidInput)
	case err != nil:
		return CountImportFile{}, fmt.Errorf("đọc tệp số đếm: %w", err)
	}
	rows, cellErrs, err := xlsx.Read(bytes.NewReader(body), int64(len(body)),
		xlsx.Limits{MaxBytes: MaxCountImportBytes, MaxRows: MaxCountImportRows}, CountImportHeader())
	switch {
	case errors.Is(err, xlsx.ErrTooLarge):
		return CountImportFile{}, fmt.Errorf("đọc tệp số đếm: %w", files.ErrTooLarge)
	case errors.Is(err, xlsx.ErrNotXlsx), errors.Is(err, xlsx.ErrHeaderMismatch), errors.Is(err, xlsx.ErrTooManyRows):
		return CountImportFile{}, fmt.Errorf("%w: tệp số đếm: %w", ErrInvalidInput, err)
	case err != nil:
		return CountImportFile{}, fmt.Errorf("đọc tệp số đếm: %w", err)
	}
	sum := sha256.Sum256(body)
	f := CountImportFile{tenantID: tenantID, fileID: fileID, sha256: hex.EncodeToString(sum[:])}
	f.rows, f.errs = parseCountRows(rows, cellErrs)
	return f, nil
}

// parseCountRows tách các dòng đọc được thành dòng hợp lệ và lỗi theo số dòng (tăng dần).
func parseCountRows(rows []xlsx.Row, cellErrs []xlsx.CellError) ([]pendingRow, []CountImportRowError) {
	out, errs := []pendingRow{}, []CountImportRowError{}
	bad := map[int]map[string]string{} // dòng → cột → mã lỗi ô
	for _, ce := range cellErrs {
		if bad[ce.Row] == nil {
			bad[ce.Row] = map[string]string{}
		}
		bad[ce.Row][ce.Column] = ce.Code
	}
	seen := map[int]bool{}
	for _, r := range rows {
		seen[r.Number] = true
		p, code := parseCountRow(r, bad[r.Number])
		if code != "" {
			errs = append(errs, CountImportRowError{Row: r.Number, Code: code})
			continue
		}
		out = append(out, p)
	}
	// Dòng chỉ có ô lỗi (mọi ô là công thức) không có trong rows: thêm lỗi cho chúng.
	for row, cols := range bad {
		if !seen[row] {
			errs = append(errs, CountImportRowError{Row: row, Code: cellErrorCode(cols)})
		}
	}
	slices.SortFunc(errs, func(a, b CountImportRowError) int { return cmp.Compare(a.Row, b.Row) })
	return out, errs
}

// parseCountRow kiểm một dòng: hợp lệ → dòng chờ và ""; không thì mã lỗi.
func parseCountRow(r xlsx.Row, bad map[string]string) (pendingRow, string) {
	if len(bad) > 0 {
		return pendingRow{}, cellErrorCode(bad)
	}
	code := strings.TrimSpace(r.Cells[colCode])
	if code == "" {
		return pendingRow{}, rowUnknownCode
	}
	n, ok := parseCount(r.Cells[colCount])
	switch {
	case !ok:
		return pendingRow{}, rowNotInteger
	case n < 0:
		return pendingRow{}, rowNegative
	}
	return pendingRow{Row: r.Number, Code: code, Qty: int32(n)}, "" //nolint:gosec // ≤ maxCounted
}

// cellErrorCode đổi lỗi ô của xlsx sang mã lỗi dòng: có ô công thức → formula; mã quá dài → unknown_code; còn lại (số
// quá dài) → not_integer.
func cellErrorCode(cols map[string]string) string {
	for _, c := range cols {
		if c == xlsx.CodeFormulaNotAllowed {
			return rowFormula
		}
	}
	if _, ok := cols[colCode]; ok {
		return rowUnknownCode
	}
	return rowNotInteger
}

// parseCount đọc số đếm: số nguyên (ô số của Excel có thể đọc ra dạng "7" hay "7.0"), trong −1 000 000…1 000 000.
func parseCount(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		x, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil || math.IsInf(x, 0) || math.IsNaN(x) || x != math.Trunc(x) || math.Abs(x) > maxCounted {
			return 0, false
		}
		n = int64(x)
	}
	if n > maxCounted || n < -maxCounted {
		return 0, false
	}
	return n, true
}

type countImportPayload struct {
	ImportID uuid.UUID `json:"importId"`
}

// CreateCountImport ghi bản nhập queued từ f vào phiên mở sổ stocktakeID với mode (replace | add) và xếp việc
// inventory.count_import, trong tx của bên gọi (tenant của f). Trả bản nhập và created = false khi cùng tệp, cùng
// phiên, mode replace đã có (bản cũ, không xếp việc mới). Lỗi: ErrNotFound (phiên không có), ErrInvalidInput (mode lạ,
// tác nhân không phải nhân viên, phiên kiểm thường, tệp của tenant khác), ErrStocktakeNotOpen, ErrImportAlreadyApplied.
// Chạy lại được trong RetryTx.
func CreateCountImport(ctx context.Context, tx pgx.Tx, jq queue.JobQueue, stocktakeID uuid.UUID, f CountImportFile,
	mode string, a Actor,
) (CountImport, bool, error) {
	switch {
	case mode != CountImportReplace && mode != CountImportAdd:
		return CountImport{}, false, fmt.Errorf("%w: chế độ nhập %q", ErrInvalidInput, mode)
	case a.Type != ActorStaff || a.ID == uuid.Nil:
		return CountImport{}, false, fmt.Errorf("%w: người nhập phải là nhân viên", ErrInvalidInput)
	case f.sha256 == "":
		return CountImport{}, false, fmt.Errorf("%w: tệp chưa chuẩn bị", ErrInvalidInput)
	case jq == nil:
		return CountImport{}, false, errNotWired
	}
	q := store.New(tx)
	if _, err := shareOpening(ctx, q, stocktakeID); err != nil {
		return CountImport{}, false, err
	}
	pending, err := json.Marshal(f.rows)
	if err != nil {
		return CountImport{}, false, fmt.Errorf("mã hóa dòng số đếm: %w", err)
	}
	errRows, err := json.Marshal(f.errs)
	if err != nil {
		return CountImport{}, false, fmt.Errorf("mã hóa lỗi dòng: %w", err)
	}
	row, err := q.InsertCountImport(ctx, store.InsertCountImportParams{
		FileID: f.fileID, FileSha256: f.sha256, Mode: mode, PendingRows: pending, ErrorRows: errRows, CreatedBy: a.ID,
		StocktakeID: stocktakeID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if mode == CountImportAdd {
			return CountImport{}, false, ErrImportAlreadyApplied
		}
		id, err := q.CountImportByFile(ctx, store.CountImportByFileParams{StocktakeID: stocktakeID, FileSha256: f.sha256, Mode: mode})
		if err != nil {
			return CountImport{}, false, fmt.Errorf("đọc bản nhập đã có: %w", err)
		}
		imp, err := GetCountImport(ctx, tx, id)
		return imp, false, err
	}
	if err != nil {
		return CountImport{}, false, fmt.Errorf("ghi bản nhập số đếm: %w", err)
	}
	if row.TenantID != f.tenantID {
		return CountImport{}, false, fmt.Errorf("%w: tệp của tenant khác", ErrInvalidInput)
	}
	tenant := row.TenantID
	if _, _, err := jq.Enqueue(ctx, tx, CountImportKind(), queue.NewJob{
		TenantID: &tenant, Payload: countImportPayload{ImportID: row.ID}, DedupeKey: row.ID.String(),
	}); err != nil {
		return CountImport{}, false, fmt.Errorf("xếp việc nhập số đếm: %w", err)
	}
	imp, err := GetCountImport(ctx, tx, row.ID)
	return imp, true, err
}

// GetCountImport đọc bản nhập id; không có hay của tenant khác → ErrNotFound.
func GetCountImport(ctx context.Context, tx pgx.Tx, id uuid.UUID) (CountImport, error) {
	imp, _, err := getCountImport(ctx, store.New(tx), id)
	return imp, err
}

func getCountImport(ctx context.Context, q *store.Queries, id uuid.UUID) (CountImport, []pendingRow, error) {
	r, err := q.GetCountImport(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return CountImport{}, nil, ErrNotFound
	case err != nil:
		return CountImport{}, nil, fmt.Errorf("đọc bản nhập số đếm: %w", err)
	}
	imp := CountImport{
		ID: r.ID, StocktakeID: r.StocktakeID, FileID: r.FileID, CreatedBy: r.CreatedBy, Mode: r.Mode, Status: r.Status,
		AppliedRows: int(r.AppliedRows), ErrorRows: []CountImportRowError{}, CreatedAt: r.CreatedAt.UTC(),
	}
	if r.FinishedAt.Valid {
		t := r.FinishedAt.Time.UTC()
		imp.FinishedAt = &t
	}
	if err := json.Unmarshal(r.ErrorRows, &imp.ErrorRows); err != nil {
		return CountImport{}, nil, fmt.Errorf("đọc lỗi dòng của bản nhập: %w", err)
	}
	var pending []pendingRow
	if err := json.Unmarshal(r.PendingRows, &pending); err != nil {
		return CountImport{}, nil, fmt.Errorf("đọc dòng chờ của bản nhập: %w", err)
	}
	return imp, pending, nil
}

// CountImportKind là việc inventory.count_import: transactional theo tenant (không đọc kho tệp), 8 lượt, hạn 30 s.
func CountImportKind() queue.Kind {
	return queue.Kind{Name: countImportKindName, Policy: queue.Policy{
		Mode: queue.ModeTransactional, MaxAttempts: countImportAttempts, Timeout: countImportTimeout,
	}}
}

// CountImportDeps là phụ thuộc của việc nền inventory.count_import. Việc chạy trong transaction của hàng đợi; tệp đã
// được đọc và phân tích ở PrepareCountImport (ngoài transaction), nên việc không cần kho tệp.
type CountImportDeps struct {
	// Now là đồng hồ của việc nền; nil → time.Now.
	Now func() time.Time
}

// CountImportJobs trả handler của việc nền inventory.count_import cho worker.
func CountImportJobs(d CountImportDeps) []queue.Handler {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return []queue.Handler{countImportJob{now: now}}
}

type countImportJob struct{ now func() time.Time }

func (countImportJob) Kind() queue.Kind { return CountImportKind() }

// Handle áp bản nhập của việc j trong tx. Bản nhập không có hay đã kết thúc → xong, không làm gì.
func (h countImportJob) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	var pl countImportPayload
	if j.TenantID == nil || json.Unmarshal(j.Payload, &pl) != nil || pl.ImportID == uuid.Nil {
		return nil, fmt.Errorf("nhập số đếm: payload hỏng: %w", queue.ErrPermanent)
	}
	q := store.New(tx)
	imp, pending, err := getCountImport(ctx, q, pl.ImportID)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, err
	case imp.Status == CountImportDone || imp.Status == CountImportFailed:
		return nil, nil
	}
	now := h.now().UTC()
	st, err := shareOpening(ctx, q, imp.StocktakeID)
	switch {
	case errors.Is(err, ErrStocktakeNotOpen), errors.Is(err, ErrInvalidInput):
		return nil, finishCountImport(ctx, q, imp.ID, CountImportFailed, 0, imp.ErrorRows, now)
	case err != nil:
		return nil, err
	}
	applied, errs, err := applyCountRows(ctx, tx, imp, st.WarehouseID, pending, now)
	if err != nil {
		return nil, err
	}
	return nil, finishCountImport(ctx, q, imp.ID, CountImportDone, applied, append(slices.Clone(imp.ErrorRows), errs...), now)
}

// OnTerminal implements queue.TerminalHook: việc hết lượt (dead) hay hết hạn → bản nhập chưa xong thành failed.
func (h countImportJob) OnTerminal(ctx context.Context, j queue.Job, _ string, tx pgx.Tx) error {
	var pl countImportPayload
	if json.Unmarshal(j.Payload, &pl) != nil || pl.ImportID == uuid.Nil {
		return nil
	}
	q := store.New(tx)
	imp, _, err := getCountImport(ctx, q, pl.ImportID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := finishCountImport(ctx, q, imp.ID, CountImportFailed, 0, imp.ErrorRows, h.now().UTC()); err != nil &&
		!errors.Is(err, errImportFinished) {
		return err
	}
	return nil
}

// variantRows là các dòng tệp của một biến thể: người bán, tổng số đếm và số dòng Excel.
type variantRows struct {
	cand  CodeCandidate
	total int64
	rows  []int
}

// applyCountRows tra mã của pending, cộng theo biến thể, upsert dòng đếm của phiên imp.StocktakeID (đã khóa chia sẻ)
// ở kho warehouseID theo variant_id tăng dần; trả số dòng tệp đã áp và lỗi dòng mới (mã lạ, mã trùng, tổng vượt
// 1 000 000). Gán lại mọi kết quả mỗi lần gọi (chạy lại được).
func applyCountRows(ctx context.Context, tx pgx.Tx, imp CountImport, warehouseID uuid.UUID, pending []pendingRow,
	now time.Time,
) (int, []CountImportRowError, error) {
	byVariant, errs, err := groupCountRows(ctx, store.New(tx), pending)
	if err != nil {
		return 0, nil, err
	}
	var ids []uuid.UUID
	for id, vr := range byVariant {
		if vr.total > maxCounted {
			errs = appendRowErrs(errs, vr.rows, rowNotInteger)
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return 0, errs, nil
	}
	slices.SortFunc(ids, compareUUID)
	done, err := upsertCountLines(ctx, tx, imp, warehouseID, ids, byVariant, now)
	if err != nil {
		return 0, nil, err
	}
	applied := 0
	for _, id := range ids {
		if done[id] {
			applied += len(byVariant[id].rows)
		} else { // add vượt 1 000 000
			errs = appendRowErrs(errs, byVariant[id].rows, rowNotInteger)
		}
	}
	return applied, errs, nil
}

// groupCountRows tra mã của pending và cộng các dòng theo biến thể; mã lạ, mã trùng thành lỗi dòng.
func groupCountRows(ctx context.Context, q *store.Queries, pending []pendingRow,
) (map[uuid.UUID]*variantRows, []CountImportRowError, error) {
	codes := make([]string, 0, len(pending))
	for _, p := range pending {
		codes = append(codes, p.Code)
	}
	slices.Sort(codes)
	found, err := resolveCodes(ctx, q, slices.Compact(codes))
	if err != nil {
		return nil, nil, err
	}
	var errs []CountImportRowError
	byVariant := map[uuid.UUID]*variantRows{}
	for _, p := range pending {
		cands := found[p.Code]
		switch {
		case len(cands) == 0:
			errs = append(errs, CountImportRowError{Row: p.Row, Code: rowUnknownCode})
			continue
		case len(cands) > 1:
			errs = append(errs, CountImportRowError{Row: p.Row, Code: rowAmbiguousCode})
			continue
		}
		vr := byVariant[cands[0].VariantID]
		if vr == nil {
			vr = &variantRows{cand: cands[0]}
			byVariant[cands[0].VariantID] = vr
		}
		vr.total += int64(p.Qty)
		vr.rows = append(vr.rows, p.Row)
	}
	return byVariant, errs, nil
}

// upsertCountLines ghi dòng đếm của các biến thể ids (đã sắp tăng dần) trong một câu, chụp expected_on_shelf =
// on_hand − in_transit; trả các biến thể đã ghi.
func upsertCountLines(ctx context.Context, tx pgx.Tx, imp CountImport, warehouseID uuid.UUID, ids []uuid.UUID,
	byVariant map[uuid.UUID]*variantRows, now time.Time,
) (map[uuid.UUID]bool, error) {
	bs, err := Availability(ctx, tx, warehouseID, ids)
	if err != nil {
		return nil, err
	}
	p := store.UpsertStocktakeLinesParams{
		Now: now, CountedBy: pgtype.UUID{Bytes: imp.CreatedBy, Valid: true}, StocktakeID: imp.StocktakeID,
		AddMode: imp.Mode == CountImportAdd,
	}
	for i, id := range ids { // Availability trả theo variant_id tăng dần, cùng thứ tự với ids
		vr := byVariant[id]
		p.VariantIds = append(p.VariantIds, id)
		p.SellerIds = append(p.SellerIds, vr.cand.SellerID)
		p.Expected = append(p.Expected, bs[i].OnHand-bs[i].InTransit)
		p.Totals = append(p.Totals, int32(vr.total)) //nolint:gosec // ≤ maxCounted
	}
	got, err := store.New(tx).UpsertStocktakeLines(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("ghi dòng đếm của bản nhập: %w", err)
	}
	done := make(map[uuid.UUID]bool, len(got))
	for _, id := range got {
		done[id] = true
	}
	return done, nil
}

func appendRowErrs(errs []CountImportRowError, rows []int, code string) []CountImportRowError {
	for _, r := range rows {
		errs = append(errs, CountImportRowError{Row: r, Code: code})
	}
	return errs
}

// finishCountImport đặt bản nhập id còn chờ sang status với số dòng đã áp và lỗi dòng (theo số dòng tăng dần); đã kết
// thúc → errImportFinished (transaction lùi).
func finishCountImport(ctx context.Context, q *store.Queries, id uuid.UUID, status string, applied int,
	errs []CountImportRowError, now time.Time,
) error {
	if errs == nil {
		errs = []CountImportRowError{}
	}
	slices.SortStableFunc(errs, func(a, b CountImportRowError) int { return cmp.Compare(a.Row, b.Row) })
	b, err := json.Marshal(errs)
	if err != nil {
		return fmt.Errorf("mã hóa lỗi dòng: %w", err)
	}
	n, err := q.FinishCountImport(ctx, store.FinishCountImportParams{
		Status: status, AppliedRows: int32(min(applied, math.MaxInt32)), ErrorRows: b, //nolint:gosec // đã chặn trên
		Now: pgtype.Timestamptz{Time: now, Valid: true}, ID: id,
	})
	switch {
	case err != nil:
		return fmt.Errorf("kết thúc bản nhập số đếm: %w", err)
	case n == 0:
		return errImportFinished
	}
	return nil
}
