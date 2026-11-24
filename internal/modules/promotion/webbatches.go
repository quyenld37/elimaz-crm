package promotion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/promotion/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
)

// Loại đợt mã (cột coupon_batches.purpose, migration 0505; kế hoạch M5, Task 197).
const (
	PurposeManual   = "manual"   // nhân viên tạo (M2)
	PurposeBirthday = "birthday" // website xin phát quà sinh nhật
	PurposeImported = "imported" // mã còn hạn nhập từ website
)

// Giới hạn của đường website (hợp đồng API: webCreateBirthdayBatch, webImportCoupons).
const (
	maxWebBatchQuantity = 5000
	maxImportItems      = 500
	maxImportPercent    = 100
	importChannel       = "web"
	defaultImportBatch  = "Mã nhập từ website"
	webBatchNameKey     = "coupon_batches_web_name_key"
	uniqueViolation     = "23505"
)

// WebBatchInput là lô mã website xin phát: Quantity (1–5.000) mã dùng một lần theo Template; BoundPhones khác rỗng thì
// đúng Quantity SĐT khác nhau, mỗi SĐT một mã. Purpose hiện chỉ nhận PurposeBirthday.
type WebBatchInput struct {
	Name, Purpose string
	Quantity      int
	Template      CouponTemplate
	BoundPhones   []string
}

// EnsureWebBatch trả lô (Purpose, Name) của website trong tx của bên gọi: đã có cùng số lượng, điều khoản và SĐT → lô
// cũ và mã cũ (created false); đã có mà khác → ErrBatchConflict; chưa có → Writer.CreateBatch của M2 (bộ sinh mã,
// mỗi mã một lượt) trong một savepoint rồi đặt purpose (created true). Hai lần gọi cùng tên chạy song song: lần sau
// gặp unique coupon_batches_web_name_key khi đặt purpose → lùi savepoint, đọc lại lô vừa commit. Mã trả theo thứ tự
// BoundPhones (có SĐT) hay theo chữ (không SĐT). Đầu vào sai → ErrInvalidInput.
func (w Writer) EnsureWebBatch(ctx context.Context, tx pgx.Tx, in WebBatchInput, a Actor) (Batch, []string, bool, error) {
	name := strings.TrimSpace(in.Name)
	switch {
	case in.Purpose != PurposeBirthday:
		return Batch{}, nil, false, fmt.Errorf("loại lô của website: %w", ErrInvalidInput)
	case in.Quantity < 1 || in.Quantity > maxWebBatchQuantity:
		return Batch{}, nil, false, fmt.Errorf("số lượng mã của lô website: %w", ErrInvalidInput)
	case !validName(name):
		return Batch{}, nil, false, fmt.Errorf("tên lô của website: %w", ErrInvalidInput)
	}
	bin := BatchInput{Name: name, Quantity: in.Quantity, Template: in.Template, BoundPhones: in.BoundPhones}
	phones, err := batchPhones(bin)
	if err != nil {
		return Batch{}, nil, false, err
	}
	if b, codes, found, err := existingWebBatch(ctx, store.New(tx), in.Purpose, name, bin, phones); found || err != nil {
		return b, codes, false, err
	}
	b, err := w.createWebBatch(ctx, tx, a, bin, in.Purpose)
	if isWebNameTaken(err) {
		b, codes, found, err := existingWebBatch(ctx, store.New(tx), in.Purpose, name, bin, phones)
		if err == nil && !found {
			err = fmt.Errorf("đọc lại lô website sau khi trùng tên: %w", ErrNotFound)
		}
		return b, codes, false, err
	}
	if err != nil {
		return Batch{}, nil, false, err
	}
	codes, err := batchCodes(ctx, store.New(tx), b.ID, phones)
	return b, codes, true, err
}

// createWebBatch gọi CreateBatch của M2 trong một savepoint (db.Savepoint) rồi đặt purpose; lỗi (kể cả trùng tên) →
// lùi savepoint, tx của bên gọi vẫn dùng được.
func (w Writer) createWebBatch(ctx context.Context, tx pgx.Tx, a Actor, in BatchInput, purpose string) (Batch, error) {
	var b Batch
	err := db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
		var err error
		if b, err = w.CreateBatch(ctx, sp, a, in); err != nil {
			return err
		}
		return store.New(sp).SetBatchPurpose(ctx, store.SetBatchPurposeParams{Purpose: purpose, ID: b.ID})
	})
	if err != nil {
		return Batch{}, fmt.Errorf("ghi lô website: %w", err)
	}
	b.Purpose = purpose
	return b, nil
}

func isWebNameTaken(err error) bool {
	pg, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pg.Code == uniqueViolation && pg.ConstraintName == webBatchNameKey
}

// existingWebBatch đọc lô (purpose, name); không có → found false. Có mà khác số lượng, điều khoản, SĐT →
// ErrBatchConflict.
func existingWebBatch(ctx context.Context, q *store.Queries, purpose, name string, in BatchInput, phones []string) (
	Batch, []string, bool, error,
) {
	row, err := q.BatchByPurposeName(ctx, store.BatchByPurposeNameParams{Purpose: purpose, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, nil, false, nil
	}
	if err != nil {
		return Batch{}, nil, false, fmt.Errorf("đọc lô website: %w", err)
	}
	b, err := batchFrom(store.GetBatchRow(row))
	if err != nil {
		return Batch{}, nil, false, err
	}
	if b.Quantity != in.Quantity || !sameTemplate(b.Template, in.Template) {
		return Batch{}, nil, true, ErrBatchConflict
	}
	codes, err := batchCodes(ctx, q, b.ID, phones)
	return b, codes, true, err
}

// batchCodes trả mã của lô id: phones có SĐT → mã theo đúng thứ tự phones (bộ SĐT của lô khác phones →
// ErrBatchConflict); không SĐT → theo chữ.
func batchCodes(ctx context.Context, q *store.Queries, id uuid.UUID, phones []string) ([]string, error) {
	rows, err := q.BatchCodes(ctx, &id)
	if err != nil {
		return nil, fmt.Errorf("đọc mã của lô: %w", err)
	}
	if len(rows) != len(phones) {
		return nil, ErrBatchConflict
	}
	byPhone := make(map[string]string, len(rows))
	codes := make([]string, 0, len(rows))
	for _, r := range rows {
		byPhone[r.BoundPhone] = r.Code
		codes = append(codes, r.Code)
	}
	if phones[0] == "" {
		if len(byPhone) != 1 || byPhone[""] == "" {
			return nil, ErrBatchConflict
		}
		slices.Sort(codes)
		return codes, nil
	}
	codes = codes[:0]
	for _, p := range phones {
		c, ok := byPhone[p]
		if !ok {
			return nil, ErrBatchConflict
		}
		codes = append(codes, c)
	}
	return codes, nil
}

// sameTemplate so điều khoản đã lưu a với điều khoản gửi lên b (tên bỏ khoảng trắng hai đầu, kênh như tập).
func sameTemplate(a, b CouponTemplate) bool {
	return a.Name == strings.TrimSpace(b.Name) && a.DiscountKind == b.DiscountKind && a.Value == b.Value &&
		eqPtr(a.MaxDiscount, b.MaxDiscount) && a.MinSubtotal == b.MinSubtotal &&
		sameSet(channelsOrEmpty(a.Channels), channelsOrEmpty(b.Channels)) && eqPtr(a.PerCustomerLimit, b.PerCustomerLimit) &&
		a.ValidFrom.Equal(b.ValidFrom) && a.ValidTo.Equal(b.ValidTo)
}

// GivenMark là kết quả MarkGiven: mã (id), lúc trao đầu tiên, lần gọi này có vừa ghi không.
type GivenMark struct {
	CouponID uuid.UUID
	At       time.Time
	First    bool
}

// MarkGiven ghi lúc website trao mã code cho khách (at) nếu chưa ghi; gọi lại giữ lúc trao đầu tiên (First false).
// Chỉ mã của lô website (birthday, imported); mã khác hay không có → ErrNotFound.
func MarkGiven(ctx context.Context, tx pgx.Tx, code string, at time.Time) (GivenMark, error) {
	q := store.New(tx)
	rows, err := q.MarkCouponGiven(ctx, store.MarkCouponGivenParams{
		At: pgtype.Timestamptz{Time: at, Valid: true}, Code: code,
	})
	if err != nil {
		return GivenMark{}, fmt.Errorf("ghi lúc trao mã: %w", err)
	}
	if len(rows) == 1 {
		return GivenMark{CouponID: rows[0].ID, At: rows[0].GivenAt.Time, First: true}, nil
	}
	r, err := q.WebCouponGiven(ctx, code)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return GivenMark{}, ErrNotFound
	case err != nil:
		return GivenMark{}, fmt.Errorf("đọc lúc trao mã: %w", err)
	case !r.GivenAt.Valid:
		return GivenMark{}, fmt.Errorf("mã của lô website chưa trao sau khi ghi: %w", ErrNotFound)
	}
	return GivenMark{CouponID: r.ID, At: r.GivenAt.Time}, nil
}

// ImportItem là một mã còn hạn của website. DiscountKind percent: Value là phần trăm nguyên 1–100 (lưu phần vạn,
// ×100); fixed: số đồng. MaxDiscount 0 = không trần. BatchName rỗng → "Mã nhập từ website".
type ImportItem struct {
	Code, DiscountKind              string
	Value, MaxDiscount, MinSubtotal int64
	ValidFrom, ValidTo              time.Time
	GivenAt                         *time.Time
	Used                            bool
	BatchName                       string
}

// ImportReport là kết quả một lần nhập: số mã chèn mới, số mã đã có cùng điều khoản (nhập lại), mã đã có với điều
// khoản khác (không đổi), theo thứ tự gửi lên.
type ImportReport struct {
	Inserted, Skipped int
	Conflicts         []string
}

// importRow là ImportItem đã kiểm và đổi sang cột của coupons.
type importRow struct {
	ImportItem

	value int64 // phần vạn (percent) hay số đồng
}

// ImportCoupons nhập 1–500 mã của website trong tx của bên gọi: mỗi mã một lượt (Used → đã dùng hết), kênh web, origin
// web_import, lô loại imported theo BatchName. Lô ghi trước rồi khóa theo id tăng dần, mã ghi sau (ADR-46 nhóm 10);
// mã đã có → so điều khoản (Skipped hay Conflicts), không đổi mã có sẵn. Mã sai mẫu, trùng trong lần nhập (không phân
// biệt hoa thường), điều khoản sai, số mục ngoài 1–500 → ErrInvalidInput, không nhập mã nào.
func ImportCoupons(ctx context.Context, tx pgx.Tx, items []ImportItem, a Actor) (ImportReport, error) {
	rows, names, err := importRows(items)
	if err != nil {
		return ImportReport{}, err
	}
	q := store.New(tx)
	batchIDs, err := importBatches(ctx, q, rows, names, a)
	if err != nil {
		return ImportReport{}, err
	}
	p := store.InsertImportedCouponsParams{Channels: []string{importChannel}, CreatedBy: a.UserID}
	for _, r := range rows {
		used, given := int32(0), ""
		if r.Used {
			used = 1
		}
		if r.GivenAt != nil {
			given = r.GivenAt.UTC().Format(time.RFC3339Nano)
		}
		p.Codes, p.BatchIds = append(p.Codes, r.Code), append(p.BatchIds, batchIDs[r.BatchName])
		p.Names, p.DiscountKinds = append(p.Names, r.BatchName), append(p.DiscountKinds, r.DiscountKind)
		p.Vals, p.MaxDiscounts = append(p.Vals, r.value), append(p.MaxDiscounts, r.MaxDiscount)
		p.MinSubtotals, p.Used = append(p.MinSubtotals, r.MinSubtotal), append(p.Used, used)
		p.ValidFroms, p.ValidTos = append(p.ValidFroms, r.ValidFrom), append(p.ValidTos, r.ValidTo)
		p.GivenAts = append(p.GivenAts, given)
	}
	inserted, err := q.InsertImportedCoupons(ctx, p)
	if err != nil {
		return ImportReport{}, fmt.Errorf("ghi mã nhập từ website: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(batchIDs))
	for _, id := range batchIDs {
		ids = append(ids, id)
	}
	if err := q.RecountImportBatches(ctx, ids); err != nil {
		return ImportReport{}, fmt.Errorf("đếm lại mã của lô nhập: %w", err)
	}
	return importReport(ctx, q, rows, inserted)
}

// importRows kiểm items, đổi phần trăm sang phần vạn, làm tròn thời gian về micro giây (độ chính xác của PostgreSQL,
// để nhập lại so bằng nhau); trả tên lô theo thứ tự gặp.
func importRows(items []ImportItem) ([]importRow, []string, error) {
	if len(items) < 1 || len(items) > maxImportItems {
		return nil, nil, fmt.Errorf("số mã nhập: %w", ErrInvalidInput)
	}
	rows := make([]importRow, 0, len(items))
	seen := make(map[string]bool, len(items))
	var names []string
	for _, it := range items {
		r := importRow{ImportItem: it, value: it.Value}
		r.BatchName = strings.TrimSpace(r.BatchName)
		if r.BatchName == "" {
			r.BatchName = defaultImportBatch
		}
		r.ValidFrom, r.ValidTo = r.ValidFrom.Truncate(time.Microsecond), r.ValidTo.Truncate(time.Microsecond)
		key := strings.ToUpper(r.Code)
		switch {
		case !codePattern.MatchString(r.Code) || seen[key]:
			return nil, nil, fmt.Errorf("mã nhập sai mẫu hay trùng: %w", ErrInvalidInput)
		case r.DiscountKind == KindPercent && (r.Value < 1 || r.Value > maxImportPercent):
			return nil, nil, fmt.Errorf("phần trăm của mã nhập: %w", ErrInvalidInput)
		case r.MaxDiscount < 0:
			return nil, nil, fmt.Errorf("trần giảm của mã nhập: %w", ErrInvalidInput)
		}
		if r.DiscountKind == KindPercent {
			r.value = r.Value * (percentScale / maxImportPercent)
		}
		one := int32(1)
		if err := validateTerms(Coupon{
			Name: r.BatchName, DiscountKind: r.DiscountKind, Value: r.value, MaxDiscount: r.maxDiscount(),
			MinSubtotal: r.MinSubtotal, Channels: []string{importChannel}, MaxUses: &one, ValidFrom: r.ValidFrom,
			ValidTo: r.ValidTo, Status: StatusActive,
		}); err != nil {
			return nil, nil, fmt.Errorf("mã nhập %s: %w", r.Code, err)
		}
		seen[key] = true
		if !slices.Contains(names, r.BatchName) {
			names = append(names, r.BatchName)
		}
		rows = append(rows, r)
	}
	return rows, names, nil
}

func (r importRow) maxDiscount() *int64 {
	if r.MaxDiscount == 0 {
		return nil
	}
	return &r.MaxDiscount
}

// importBatches ghi lô imported cho mỗi tên (có rồi thì giữ), khóa các lô theo id tăng dần; trả tên → id.
func importBatches(ctx context.Context, q *store.Queries, rows []importRow, names []string, a Actor) (
	map[string]uuid.UUID, error,
) {
	for _, name := range names {
		var first importRow
		n := 0
		for _, r := range rows {
			if r.BatchName == name {
				if n == 0 {
					first = r
				}
				n++
			}
		}
		raw, err := json.Marshal(templateJSON{
			Name: name, DiscountKind: first.DiscountKind, Value: first.value, MaxDiscount: first.maxDiscount(),
			MinSubtotal: first.MinSubtotal, Channels: []string{importChannel}, ValidFrom: first.ValidFrom,
			ValidTo: first.ValidTo,
		})
		if err != nil {
			return nil, fmt.Errorf("ghi mẫu lô nhập: %w", err)
		}
		if err := q.InsertImportBatch(ctx, store.InsertImportBatchParams{
			Name: name, Quantity: int32(n), Template: raw, CreatedBy: a.UserID,
		}); err != nil {
			return nil, fmt.Errorf("ghi lô nhập: %w", err)
		}
	}
	locked, err := q.LockImportBatches(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("khóa lô nhập: %w", err)
	}
	out := make(map[string]uuid.UUID, len(locked))
	for _, l := range locked {
		out[l.Name] = l.ID
	}
	if len(out) != len(names) {
		return nil, fmt.Errorf("lô nhập vừa ghi không thấy: %w", ErrNotFound)
	}
	return out, nil
}

// importReport đếm mã đã chèn; mã không chèn được (đã có) so điều khoản với mã có sẵn.
func importReport(ctx context.Context, q *store.Queries, rows []importRow, inserted []string) (ImportReport, error) {
	rep := ImportReport{Inserted: len(inserted), Conflicts: []string{}}
	done := make(map[string]bool, len(inserted))
	for _, c := range inserted {
		done[strings.ToUpper(c)] = true
	}
	var rest []string
	for _, r := range rows {
		if !done[strings.ToUpper(r.Code)] {
			rest = append(rest, r.Code)
		}
	}
	if len(rest) == 0 {
		return rep, nil
	}
	have, err := q.CouponTermsByCodes(ctx, rest)
	if err != nil {
		return ImportReport{}, fmt.Errorf("đọc mã có sẵn trùng mã nhập: %w", err)
	}
	terms := make(map[string]store.CouponTermsByCodesRow, len(have))
	for _, h := range have {
		terms[strings.ToUpper(h.Code)] = h
	}
	for _, r := range rows {
		key := strings.ToUpper(r.Code)
		if done[key] {
			continue
		}
		h, ok := terms[key]
		if ok && h.DiscountKind == r.DiscountKind && h.Value == r.value && h.MaxDiscount == r.MaxDiscount &&
			h.MinSubtotal == r.MinSubtotal && h.ValidFrom.Equal(r.ValidFrom) && h.ValidTo.Equal(r.ValidTo) {
			rep.Skipped++
			continue
		}
		rep.Conflicts = append(rep.Conflicts, r.Code)
	}
	return rep, nil
}

// ListBatches trả một trang đợt mã, mới nhất trước (purpose rỗng: mọi loại), và con trỏ trang sau (nil: hết).
func ListBatches(ctx context.Context, tx pgx.Tx, purpose string, after *uuid.UUID, limit int) ([]Batch, *uuid.UUID, error) {
	limit = pageLimit(limit)
	p := store.ListBatchesParams{After: after, RowLimit: int32(limit + 1)} //nolint:gosec // G115: limit ≤ 100
	if purpose != "" {
		p.Purpose = pgtype.Text{String: purpose, Valid: true}
	}
	rows, err := store.New(tx).ListBatches(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách đợt mã: %w", err)
	}
	out := make([]Batch, 0, min(len(rows), limit))
	for _, r := range rows[:min(len(rows), limit)] {
		b, err := batchFrom(store.GetBatchRow(r))
		if err != nil {
			return nil, nil, err
		}
		out = append(out, b)
	}
	if len(rows) > limit {
		next := out[len(out)-1].ID
		return out, &next, nil
	}
	return out, nil, nil
}

// BatchStats là số mã của một đợt: website đã trao, đã dùng, hết hạn (ValidTo ≤ now) mà chưa dùng.
type BatchStats struct {
	Given, Used, Expired int
}

// GetBatchStats đếm mã của đợt id lúc now (đợt không có → số 0; bên gọi đọc đợt bằng GetBatch trước).
func GetBatchStats(ctx context.Context, tx pgx.Tx, id uuid.UUID, now time.Time) (BatchStats, error) {
	m, err := ListBatchStats(ctx, tx, []uuid.UUID{id}, now)
	if err != nil {
		return BatchStats{}, err
	}
	return m[id], nil
}

// ListBatchStats đếm mã của các đợt ids lúc now trong một câu (danh sách đợt, Task 487). Đợt không có mã hay không có
// thì không có trong map (bên gọi đọc số 0).
func ListBatchStats(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, now time.Time) (map[uuid.UUID]BatchStats, error) {
	out := make(map[uuid.UUID]BatchStats, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := store.New(tx).BatchStatsMany(ctx, store.BatchStatsManyParams{Now: now, BatchIds: ids})
	if err != nil {
		return nil, fmt.Errorf("đếm mã của đợt: %w", err)
	}
	for _, r := range rows {
		out[r.BatchID] = BatchStats{Given: int(r.Given), Used: int(r.Used), Expired: int(r.Expired)}
	}
	return out, nil
}
