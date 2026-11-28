package customer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

const (
	importKindName = "customer.import"
	// importAttempts: mỗi lượt tự dừng sau importBudget và hẹn lượt sau (not_sent), nên một tệp 20.000 dòng cần vài
	// lượt (đo ở Task M2-13: ~7 ms/dòng trên máy dev → ~7 lượt); 60 lượt × 20 s = 20 phút việc.
	importAttempts = 60
	importTimeout  = 30 * time.Second
	importBudget   = 20 * time.Second
	importYield    = time.Second
	// importRetry: chờ trước lượt sau khi lỗi (database, kho tệp). Cố định thay lịch lùi dần của hàng đợi, vì số lượt
	// đã tăng theo các lần tự dừng (lịch lùi dần sẽ chờ hàng giờ).
	importRetry = 30 * time.Second
)

// Lý do ngắn ghi vào last_error của lệnh (không dữ liệu cá nhân).
const (
	reasonBadPayload = "bad_payload"
	reasonNotWired   = "not_wired"
	reasonNoImport   = "import_not_found"
	reasonLoad       = "load_failed"
	reasonRead       = "read_failed"
	reasonBatch      = "batch_failed"
	reasonYield      = "yield"
	reasonStale      = "concurrent_run"
)

// errStaleProgress: số dòng đã xử lý của lần nhập khác vị trí lượt này đang ghi (một lượt khác đã chạy song song);
// transaction lô lùi, lượt này dừng.
var errStaleProgress = errors.New("customer: tiến độ lần nhập đã đổi")

// ImportKind là lệnh customer.import: Mode command (chạy ngoài transaction, đọc kho tệp), hết giữ chỗ thì chạy lại
// (lệnh chỉ đọc kho, mọi ghi có điều kiện nên chạy lại an toàn), 60 lượt, hạn một lần 30 s.
func ImportKind() queue.Kind {
	return queue.Kind{Name: importKindName, Policy: queue.Policy{
		Mode: queue.ModeCommand, MaxAttempts: importAttempts, OnLeaseExpired: queue.LeaseRetry, Timeout: importTimeout,
	}}
}

type importPayload struct {
	ImportID uuid.UUID `json:"importId"`
}

// ImporterOptions là phụ thuộc của lệnh nhập: DB (crm_app), Store (kho tệp), Writer (sự kiện customer.created, audit).
type ImporterOptions struct {
	DB     *db.DB
	Store  files.FileStore
	Writer Writer
}

// Importer là lệnh customer.import (commands.Handler, queue.TerminalHook). Execute, ngoài transaction: đọc lần nhập,
// đọc tệp (files.ReadContent) và phân tích (xlsx.Read), đọc danh mục hành chính, đặt running với tổng số dòng, rồi
// mỗi lô 500 dòng một transaction: tạo khách cho dòng hợp lệ có SĐT chưa có, SĐT đã có → skipped (không ghi đè), dòng
// lỗi → {row, column, code}; cùng transaction cộng số đếm và nối lỗi (nên chạy lại tiếp từ dòng ok+skipped+error).
// Cuối: done kèm đúng một dòng audit customer.import (số đếm). Mỗi lượt dừng sau 20 s và hẹn lượt sau.
type Importer struct {
	o          ImporterOptions
	budget     time.Duration
	afterBatch func(processed int) error // test: chạy sau mỗi lô đã commit
}

// NewImporter dựng lệnh nhập khách.
func NewImporter(o ImporterOptions) *Importer { return &Importer{o: o, budget: importBudget} }

// Kind implements commands.Handler.
func (*Importer) Kind() queue.Kind { return ImportKind() }

// AutoRetryNotSent implements commands.Handler: chạy lại an toàn.
func (*Importer) AutoRetryNotSent() bool { return true }

func notSent(reason string) commands.Outcome {
	return commands.Outcome{Status: commands.StatusNotSent, Reason: reason, RetryAfter: importRetry}
}

// Execute implements commands.Handler.
func (i *Importer) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	started := time.Now()
	var pl importPayload
	if j.TenantID == nil || json.Unmarshal(j.Payload, &pl) != nil || pl.ImportID == uuid.Nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	if i.o.Writer.check() != nil || i.o.DB == nil || i.o.Store == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNotWired}, nil
	}
	tenant := *j.TenantID
	imp, err := i.load(ctx, tenant, pl.ImportID)
	switch {
	case errors.Is(err, ErrNotFound):
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoImport}, nil
	case err != nil:
		return notSent(reasonLoad), nil
	case imp.Status == ImportDone || imp.Status == ImportFailed:
		return commands.Outcome{Status: commands.StatusDone}, nil
	}
	rows, cellErrs, err := readImportFile(ctx, i.o.DB, i.o.Store, tenant, imp.FileID)
	switch {
	case errors.Is(err, ErrImportTooLarge):
		return i.fail(ctx, tenant, imp.ID, codeImportTooLarge)
	case errors.Is(err, ErrImportFileInvalid):
		return i.fail(ctx, tenant, imp.ID, codeImportFileBroken)
	case err != nil:
		return notSent(reasonRead), nil
	}
	var units *adminIndex
	if err := tenancy.WithTenant(ctx, i.o.DB, tenant, func(tx pgx.Tx) error {
		var err error
		units, err = loadAdminIndex(ctx, tx)
		return err
	}); err != nil {
		return notSent(reasonLoad), nil
	}
	if imp, err = i.begin(ctx, tenant, imp.ID, len(rows)); err != nil {
		return notSent(reasonLoad), nil
	}
	if imp.Status != ImportRunning { // lượt khác vừa kết thúc lần nhập
		return commands.Outcome{Status: commands.StatusDone}, nil
	}
	return i.process(ctx, tenant, imp, rows, groupCellErrors(cellErrs), units, started)
}

// process chạy các lô còn lại rồi kết thúc lần nhập.
func (i *Importer) process(ctx context.Context, tenant uuid.UUID, imp Import, rows []xlsx.Row,
	cellErrs map[int][]xlsx.CellError, units *adminIndex, started time.Time,
) (commands.Outcome, error) {
	for offset := imp.processed(); offset < len(rows); {
		// Hết ngân sách thì hẹn lượt sau; lượt nào cũng làm ít nhất một lô (offset > imp.processed()).
		if offset > imp.processed() && time.Since(started) > i.budget {
			return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonYield, RetryAfter: importYield}, nil
		}
		end := min(offset+ImportBatchSize, len(rows))
		err := i.batch(ctx, tenant, imp, rows[offset:end], cellErrs, units, offset)
		switch {
		case errors.Is(err, errStaleProgress):
			return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonStale, RetryAfter: importYield}, nil
		case err != nil:
			return notSent(reasonBatch), nil
		}
		offset = end
		if i.afterBatch != nil {
			if err := i.afterBatch(offset); err != nil {
				return commands.Outcome{}, err
			}
		}
	}
	err := i.finish(ctx, tenant, imp.ID, len(rows))
	switch {
	case errors.Is(err, errStaleProgress):
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonStale, RetryAfter: importYield}, nil
	case err != nil:
		return notSent(reasonBatch), nil
	}
	return commands.Outcome{Status: commands.StatusDone}, nil
}

func (i *Importer) load(ctx context.Context, tenant, id uuid.UUID) (Import, error) {
	var imp Import
	err := tenancy.WithTenant(ctx, i.o.DB, tenant, func(tx pgx.Tx) error {
		var err error
		imp, err = GetImport(ctx, tx, id)
		return err
	})
	return imp, err //nolint:wrapcheck // GetImport đã bọc
}

// begin đặt lần nhập pending sang running với tổng số dòng (lượt đầu) và trả lần nhập đọc lại.
func (i *Importer) begin(ctx context.Context, tenant, id uuid.UUID, total int) (Import, error) {
	var imp Import
	err := tenancy.WithTenant(ctx, i.o.DB, tenant, func(tx pgx.Tx) error {
		q := store.New(tx)
		if err := q.StartImportRun(ctx, store.StartImportRunParams{ID: id, TotalRows: int4(total)}); err != nil {
			return fmt.Errorf("bắt đầu lần nhập: %w", err)
		}
		var err error
		imp, err = GetImport(ctx, tx, id)
		return err
	})
	return imp, err //nolint:wrapcheck // đã bọc ở trên
}

// fail đặt lần nhập failed với một lỗi cả tệp (row 0); lệnh xong (done).
func (i *Importer) fail(ctx context.Context, tenant, id uuid.UUID, code string) (commands.Outcome, error) {
	if err := tenancy.WithTenant(ctx, i.o.DB, tenant, func(tx pgx.Tx) error {
		return finishImport(ctx, tx, id, ImportFailed, []ImportError{{Code: code}})
	}); err != nil {
		return notSent(reasonBatch), nil
	}
	return commands.Outcome{Status: commands.StatusDone}, nil
}

// OnTerminal implements queue.TerminalHook: lệnh hết lượt (dead), hết hạn hay in_doubt → lần nhập chưa xong thành
// failed với lỗi cả tệp import_failed.
func (i *Importer) OnTerminal(ctx context.Context, j queue.Job, _ string, tx pgx.Tx) error {
	var pl importPayload
	if json.Unmarshal(j.Payload, &pl) != nil || pl.ImportID == uuid.Nil {
		return nil
	}
	return finishImport(ctx, tx, pl.ImportID, ImportFailed, []ImportError{{Code: codeImportAbandoned}})
}

func finishImport(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, errs []ImportError) error {
	if errs == nil {
		errs = []ImportError{} // "null" nối vào mảng jsonb sẽ thành một phần tử
	}
	b, err := json.Marshal(errs)
	if err != nil {
		return fmt.Errorf("mã hóa lỗi lần nhập: %w", err)
	}
	if err := store.New(tx).FinishImport(ctx, store.FinishImportParams{
		ID: id, Status: status, NewErrors: b, Now: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("kết thúc lần nhập: %w", err)
	}
	return nil
}

// finish kết thúc lần nhập đã xử lý đủ total dòng: done + một dòng audit customer.import (số đếm). Đã kết thúc → không
// làm gì; còn thiếu dòng → errStaleProgress.
func (i *Importer) finish(ctx context.Context, tenant, id uuid.UUID, total int) error {
	return tenancy.WithTenant(ctx, i.o.DB, tenant, func(tx pgx.Tx) error { //nolint:wrapcheck // lỗi bên trong đã bọc
		row, err := store.New(tx).LockImport(ctx, id)
		if err != nil {
			return fmt.Errorf("khóa lần nhập: %w", err)
		}
		imp, err := importOf(row)
		switch {
		case err != nil:
			return err
		case imp.Status != ImportRunning:
			return nil
		case imp.processed() != total:
			return errStaleProgress
		}
		if err := finishImport(ctx, tx, id, ImportDone, nil); err != nil {
			return err
		}
		a := Actor{Type: ActorStaff, ID: imp.CreatedBy}
		return i.o.Writer.auditEntity(ctx, tx, row.TenantID, a, "customer.import", "customer.import", id, nil,
			map[string]any{
				"total": imp.TotalRows, "ok": imp.OKRows, "skipped": imp.SkippedRows, "errors": imp.ErrorRows,
				"file_id": imp.FileID.String(),
			})
	})
}

// groupCellErrors gom lỗi ô của xlsx theo số dòng.
func groupCellErrors(errs []xlsx.CellError) map[int][]xlsx.CellError {
	m := map[int][]xlsx.CellError{}
	for _, e := range errs {
		m[e.Row] = append(m[e.Row], e)
	}
	return m
}

// batchResult là số đếm và lỗi của một lô.
type batchResult struct {
	ok, skipped int
	errs        []ImportError
}

// batch ghi một lô trong một transaction (chạy lại được: mọi kết quả gán lại trong thân hàm): tạo khách, rồi khóa
// lần nhập (ADR-46: customer_imports cuối nhóm 5), so tiến độ với start, cộng số đếm và nối lỗi (tối đa
// MaxImportErrors lỗi lưu).
func (i *Importer) batch(ctx context.Context, tenant uuid.UUID, imp Import, rows []xlsx.Row,
	cellErrs map[int][]xlsx.CellError, units *adminIndex, start int,
) error {
	a := Actor{Type: ActorStaff, ID: imp.CreatedBy}
	return tenancy.WithTenant(ctx, i.o.DB, tenant, func(tx pgx.Tx) error { //nolint:wrapcheck // lỗi bên trong đã bọc
		now := time.Now().UTC()
		res, err := i.writeRows(ctx, tx, rows, cellErrs, units, a, now)
		if err != nil {
			return err
		}
		q := store.New(tx)
		row, err := q.LockImport(ctx, imp.ID)
		if err != nil {
			return fmt.Errorf("khóa lần nhập: %w", err)
		}
		cur, err := importOf(row)
		switch {
		case err != nil:
			return err
		case cur.Status != ImportRunning || cur.processed() != start:
			return errStaleProgress
		}
		keep := append([]ImportError{}, res.errs[:min(len(res.errs), max(0, MaxImportErrors-len(cur.Errors)))]...)
		b, err := json.Marshal(keep)
		if err != nil {
			return fmt.Errorf("mã hóa lỗi lô: %w", err)
		}
		if err := q.SaveImportProgress(ctx, store.SaveImportProgressParams{
			ID: imp.ID, Ok: int32(res.ok), Skipped: int32(res.skipped), Failed: int32(len(res.errs)), NewErrors: b, //nolint:gosec // ≤ 500 dòng một lô
		}); err != nil {
			return fmt.Errorf("ghi tiến độ lần nhập: %w", err)
		}
		return nil
	})
}

// writeRows kiểm và ghi các dòng của lô trong tx.
func (i *Importer) writeRows(ctx context.Context, tx pgx.Tx, rows []xlsx.Row, cellErrs map[int][]xlsx.CellError,
	units *adminIndex, a Actor, now time.Time,
) (batchResult, error) {
	var res batchResult
	valid := make([]importRowInput, 0, len(rows))
	for _, r := range rows {
		in, rowErr := parseImportRow(r, cellErrs[r.Number], units, now)
		if rowErr != nil {
			res.errs = append(res.errs, *rowErr)
			continue
		}
		valid = append(valid, in)
	}
	q := store.New(tx)
	phones := make([]string, len(valid))
	for k, in := range valid {
		phones[k] = string(in.phone)
	}
	have, err := q.ExistingPhones(ctx, phones)
	if err != nil {
		return batchResult{}, fmt.Errorf("tra SĐT đã có: %w", err)
	}
	taken := make(map[string]bool, len(have))
	for _, p := range have {
		taken[p] = true
	}
	var fresh []importRowInput
	for _, in := range valid {
		if taken[string(in.phone)] {
			res.skipped++
			continue
		}
		taken[string(in.phone)] = true // SĐT lặp lại trong cùng tệp: dòng sau skipped
		fresh = append(fresh, in)
	}
	tags, err := ensureTags(ctx, q, fresh)
	if err != nil {
		return batchResult{}, err
	}
	for _, in := range fresh {
		_, created, err := i.importRow(ctx, tx, in, tags, a, now)
		switch {
		case err != nil:
			return batchResult{}, err
		case created:
			res.ok++
		default:
			res.skipped++
		}
	}
	slices.SortFunc(res.errs, func(x, y ImportError) int { return x.Row - y.Row })
	return res, nil
}

// ensureTags tạo nhãn chưa có (màu gray) cho mọi tên nhãn của rows và trả tên → mã.
func ensureTags(ctx context.Context, q *store.Queries, rows []importRowInput) (map[string]uuid.UUID, error) {
	var names []string
	for _, in := range rows {
		names = append(names, in.tags...)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	if len(names) == 0 {
		return nil, nil
	}
	for _, n := range names {
		if err := q.InsertTagIfAbsent(ctx, n); err != nil {
			return nil, fmt.Errorf("tạo nhãn: %w", err)
		}
	}
	got, err := q.TagIDsByName(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("đọc nhãn: %w", err)
	}
	out := make(map[string]uuid.UUID, len(got))
	for _, g := range got {
		out[g.Name] = g.ID
	}
	return out, nil
}

// importRow tạo một khách từ dòng in trong savepoint: hồ sơ (lead), danh tính phone nguồn import, địa chỉ mặc định,
// nhãn, ghi chú, sự kiện customer.created. Không audit, không báo hiệu từng khách. SĐT vừa bị chiếm (giao dịch song
// song) hay mã khách cũ vừa được ghi (lượt chuyển song song) → lùi savepoint, trả false. Trả mã khách vừa tạo.
// Khách chuyển từ hệ cũ có thêm SĐT phụ (SĐT đã có chủ thì bỏ qua) và dòng đối chiếu mã cũ.
func (i *Importer) importRow(ctx context.Context, tx pgx.Tx, in importRowInput, tags map[string]uuid.UUID, a Actor,
	now time.Time,
) (uuid.UUID, bool, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("sinh mã khách: %w", err)
	}
	err = db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
		q := store.New(sp)
		row, err := q.InsertCustomerProfile(ctx, store.InsertCustomerProfileParams{
			ID: id, FullName: in.name, NameSearch: NormalizeName(in.name), Lifecycle: string(LifecycleLead),
			Birthday: dateOf(in.birthday), CreatedBy: a.createdBy(), Now: now,
		})
		if err != nil {
			return fmt.Errorf("ghi khách nhập: %w", err)
		}
		_, _, owner, err := insertIdentity(ctx, sp, id, IdentityPhone, "", string(in.phone), SourceImport, a)
		switch {
		case err != nil:
			return err
		case owner != nil:
			return errPhoneTaken
		}
		if err := importExtras(ctx, q, id, in, tags, a, now); err != nil {
			return err
		}
		if err := importLegacy(ctx, sp, q, id, in, a); err != nil {
			return err
		}
		if _, _, err := i.o.Writer.Events.Emit(ctx, sp, i.o.Writer.Queue, events.Event{
			TenantID: row.TenantID, AggregateType: aggregateCustomer, AggregateID: id.String(), Seq: row.Version,
			Topic: TopicCustomerCreated, Payload: Created{CustomerID: id, Version: row.Version},
		}); err != nil {
			return fmt.Errorf("phát %s: %w", TopicCustomerCreated, err)
		}
		return nil
	})
	switch {
	case errors.Is(err, errPhoneTaken), errors.Is(err, errLegacyTaken):
		return uuid.Nil, false, nil
	case err != nil:
		return uuid.Nil, false, err //nolint:wrapcheck // lỗi bên trong đã bọc; lỗi savepoint do db bọc
	}
	return id, true, nil
}

// importExtras ghi địa chỉ mặc định, nhãn, ghi chú của khách id vừa tạo từ dòng in.
func importExtras(ctx context.Context, q *store.Queries, id uuid.UUID, in importRowInput, tags map[string]uuid.UUID,
	a Actor, now time.Time,
) error {
	if in.addr != nil {
		if _, err := q.InsertAddress(ctx, insertAddressParams(id, *in.addr, true, now)); err != nil {
			return fmt.Errorf("ghi địa chỉ khách nhập: %w", err)
		}
	}
	for _, name := range in.tags {
		if _, err := q.InsertTagLink(ctx, store.InsertTagLinkParams{
			CustomerID: id, TagID: tags[name], Source: TagSourceStaff, CreatedBy: a.createdBy(), Now: now,
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("gắn nhãn khách nhập: %w", err)
		}
	}
	if in.note != "" {
		if _, err := q.InsertNote(ctx, store.InsertNoteParams{
			CustomerID: id, Body: in.note, AuthorID: a.ID, Now: now,
		}); err != nil {
			return fmt.Errorf("ghi chú khách nhập: %w", err)
		}
	}
	return nil
}

func int4(n int) pgtype.Int4 { return pgtype.Int4{Int32: int32(n), Valid: true} } //nolint:gosec // ≤ 20.000 dòng
