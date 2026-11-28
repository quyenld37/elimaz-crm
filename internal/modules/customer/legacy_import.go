package customer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Chuyển khách từ hệ cũ (kế hoạch M7, Task 542; ADR-48). Module không biết hệ cũ là gì: lệnh vận hành đưa vào danh sách
// LegacyCustomer đã đọc xong. Luật: SĐT là khóa (không bao giờ hai hồ sơ cùng SĐT); khách đã có mã cũ bỏ qua (chạy lại
// không trùng); SĐT đã là của khách CRM thì không ghi đè; trường hợp không chắc không tự gộp mà ghi vào
// customer.legacy_review (Task 68: gộp chỉ do nhân viên).

// Lý do một khách cũ vào danh sách cần xem (CHECK của customer.legacy_review.reason).
const (
	// LegacyReviewPhonesSplit: các SĐT của khách cũ thuộc từ hai khách CRM khác nhau.
	LegacyReviewPhonesSplit = "phones_split"
	// LegacyReviewDuplicate: SĐT đã thuộc khách đến từ một khách cũ khác (hai khách cũ cùng SĐT).
	LegacyReviewDuplicate = "duplicate_in_source"
	// LegacyReviewPhoneNotAdded: SĐT chính đã là của một khách CRM, còn SĐT khác của khách cũ chưa được thêm.
	LegacyReviewPhoneNotAdded = "phone_not_added"
)

const (
	defaultLegacyName = "Khách cũ"
	minBirthYear      = 1900
	// Cột trong mã lỗi dòng của lần chuyển: tên trường của nguồn, không bao giờ kèm giá trị.
	legacyColID    = "id"
	legacyColPhone = "mobile"
)

var errLegacyTaken = errors.New("customer: mã khách cũ đã được chuyển")

// errLegacyDryRun lùi transaction của lần chạy khô.
var errLegacyDryRun = errors.New("customer: chạy khô, lùi")

// LegacyCustomer là một khách của hệ cũ đã đọc xong, chưa kiểm. Row là số thứ tự trong bản chụp (từ 1) dùng cho lỗi.
// Name, Phones, Tags, Note, Birthday là dữ liệu cá nhân: ghi log luôn ra "[Redacted]".
type LegacyCustomer struct {
	Row      int
	LegacyID int64
	Name     string
	Phones   []string
	Birthday *time.Time
	Tags     []string
	Note     string
}

// LogValue che toàn bộ khách (chỉ giữ không gì).
func (LegacyCustomer) LogValue() slog.Value { return slog.StringValue(redacted) }

// LegacyResult là kết quả một lần chuyển. Mỗi khách rơi đúng một ô: OK (tạo mới), Skipped (đã chuyển hay SĐT đã là của
// khách CRM, không việc gì), Review (cần nhân viên xem, ghi ở customer.legacy_review), Failed (lỗi dòng).
type LegacyResult struct {
	Total, OK, Skipped, Review, Failed int
	// Errors là lỗi dòng theo thứ tự dòng, tối đa MaxImportErrors (Failed vẫn đếm đủ).
	Errors []ImportError
}

func (r *LegacyResult) add(o LegacyResult) {
	r.Total += o.Total
	r.OK += o.OK
	r.Skipped += o.Skipped
	r.Review += o.Review
	r.Failed += o.Failed
	r.Errors = append(r.Errors, o.Errors[:min(len(o.Errors), max(0, MaxImportErrors-len(r.Errors)))]...)
}

// LegacyImporter chuyển khách từ hệ cũ bằng đường tạo khách của lần nhập Excel (importRow).
type LegacyImporter struct {
	DB     *db.DB
	Writer Writer
}

// Run chuyển rows vào tenant, mỗi lô ImportBatchSize khách một transaction (lô đã commit giữ nguyên khi lượt chết giữa
// chừng; chạy lại tiếp tục nhờ mã cũ và SĐT), rồi ghi MỘT dòng audit customer.legacy_import chỉ có số đếm. dryRun:
// cùng đường xử lý nhưng toàn bộ trong một transaction rồi lùi, không ghi gì (kể cả audit).
func (l LegacyImporter) Run(ctx context.Context, tenant uuid.UUID, a Actor, rows []LegacyCustomer, dryRun bool) (LegacyResult, error) {
	if err := l.Writer.check(); err != nil {
		return LegacyResult{}, err
	}
	if l.DB == nil {
		return LegacyResult{}, errWriterNotWired
	}
	if len(rows) == 0 {
		return LegacyResult{Errors: []ImportError{}}, nil
	}
	imp := NewImporter(ImporterOptions{Writer: l.Writer})
	if dryRun {
		var res LegacyResult
		err := tenancy.WithTenant(ctx, l.DB, tenant, func(tx pgx.Tx) error {
			res = LegacyResult{Errors: []ImportError{}} // tx có thể chạy lại: không cộng dồn
			now := time.Now().UTC()
			for chunk := range slices.Chunk(rows, ImportBatchSize) {
				r, err := imp.legacyBatch(ctx, tx, chunk, a, now)
				if err != nil {
					return err
				}
				res.add(r)
			}
			return errLegacyDryRun
		})
		if err != nil && !errors.Is(err, errLegacyDryRun) {
			return LegacyResult{}, fmt.Errorf("chuyển khách (chạy khô): %w", err)
		}
		return res, nil
	}
	total := LegacyResult{Errors: []ImportError{}}
	for chunk := range slices.Chunk(rows, ImportBatchSize) {
		var res LegacyResult
		err := tenancy.WithTenant(ctx, l.DB, tenant, func(tx pgx.Tx) error {
			var err error
			res, err = imp.legacyBatch(ctx, tx, chunk, a, time.Now().UTC())
			return err
		})
		if err != nil {
			return total, fmt.Errorf("chuyển khách: %w", err)
		}
		total.add(res)
	}
	runID, err := uuid.NewV7()
	if err != nil {
		return total, fmt.Errorf("sinh mã lần chuyển: %w", err)
	}
	if err := tenancy.WithTenant(ctx, l.DB, tenant, func(tx pgx.Tx) error {
		return l.Writer.auditEntity(ctx, tx, tenant, a, "customer.legacy_import", "customer.legacy_import", runID, nil,
			map[string]any{
				"total": total.Total, "ok": total.OK, "skipped": total.Skipped, "review": total.Review, "errors": total.Failed,
			})
	}); err != nil {
		return total, fmt.Errorf("ghi audit lần chuyển: %w", err)
	}
	return total, nil
}

// legacyRow là một khách đã kiểm: dữ liệu để tạo và mọi SĐT chuẩn hóa (SĐT đầu là chính).
type legacyRow struct {
	in     importRowInput
	phones []logging.Phone
}

// parseLegacy kiểm một khách: mã cũ > 0, ít nhất một SĐT hợp lệ (SĐT sai trong số nhiều SĐT bị bỏ). Trường phụ sai (ngày
// sinh ngoài 1900..hôm nay, nhãn dài hay có ký tự điều khiển, ghi chú có ký tự điều khiển) bị bỏ, không làm mất khách;
// quá 20 nhãn giữ 20 đầu, ghi chú quá 4000 ký tự cắt, tên rỗng hay sai là "Khách cũ".
func parseLegacy(c LegacyCustomer, today time.Time) (legacyRow, *ImportError) {
	if c.LegacyID <= 0 {
		return legacyRow{}, &ImportError{Row: c.Row, Column: legacyColID, Code: rowRequired}
	}
	var phones []logging.Phone
	sawRaw := false
	for _, raw := range c.Phones {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		sawRaw = true
		if p, err := NormalizePhone(raw); err == nil && !slices.Contains(phones, p) {
			phones = append(phones, p)
		}
	}
	if len(phones) == 0 {
		code := rowInvalidPhone
		if !sawRaw {
			code = rowRequired
		}
		return legacyRow{}, &ImportError{Row: c.Row, Column: legacyColPhone, Code: code}
	}
	in := importRowInput{row: c.Row, phone: phones[0], morePhones: phones[1:], legacyID: c.LegacyID}
	in.name = legacyName(c.Name)
	if b := c.Birthday; b != nil && b.Year() >= minBirthYear && !b.After(today) {
		in.birthday = b
	}
	seen := map[string]bool{}
	for _, raw := range c.Tags {
		tag := strings.TrimSpace(raw)
		if _, err := validText(tag, maxTagNameRunes); err != nil || seen[tag] || len(in.tags) == maxImportTags {
			continue
		}
		seen[tag] = true
		in.tags = append(in.tags, tag)
	}
	if note := truncateRunes(strings.TrimSpace(c.Note), maxNoteRunes); note != "" {
		if v, err := validNote(note); err == nil {
			in.note = v
		}
	}
	return legacyRow{in: in, phones: phones}, nil
}

func legacyName(raw string) string {
	if v, err := validName(truncateRunes(strings.TrimSpace(raw), maxNameRunes)); err == nil {
		return v
	}
	return defaultLegacyName
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// holder là chủ hiện tại của một SĐT: gốc của nhóm và mã khách cũ của hồ sơ giữ SĐT (0: không đến từ lần chuyển).
type holder struct {
	root   uuid.UUID
	legacy int64
}

// legacyBatch xử lý một lô trong tx (chạy lại được: mọi kết quả gán trong thân hàm).
func (i *Importer) legacyBatch(ctx context.Context, tx pgx.Tx, rows []LegacyCustomer, a Actor, now time.Time,
) (LegacyResult, error) {
	res := LegacyResult{Total: len(rows), Errors: []ImportError{}}
	var valid []legacyRow
	var ids []int64
	var phones []string
	for _, c := range rows {
		r, rowErr := parseLegacy(c, now)
		if rowErr != nil {
			res.Failed++
			res.Errors = append(res.Errors, *rowErr)
			continue
		}
		valid = append(valid, r)
		ids = append(ids, c.LegacyID)
		for _, p := range r.phones {
			phones = append(phones, string(p))
		}
	}
	q := store.New(tx)
	mappedRows, err := q.LegacyMapped(ctx, ids)
	if err != nil {
		return LegacyResult{}, fmt.Errorf("tra mã khách cũ đã chuyển: %w", err)
	}
	mapped := map[int64]bool{}
	for _, id := range mappedRows {
		mapped[id] = true
	}
	held, err := q.LegacyPhoneHolders(ctx, phones)
	if err != nil {
		return LegacyResult{}, fmt.Errorf("tra chủ của SĐT: %w", err)
	}
	holders := make(map[logging.Phone]holder, len(held))
	for _, h := range held {
		holders[logging.Phone(h.Value)] = holder{root: h.RootID, legacy: h.LegacyID.Int64}
	}
	tagIDs := map[string]uuid.UUID{}
	for _, r := range valid {
		if mapped[r.in.legacyID] {
			res.Skipped++
			continue
		}
		verdict, reason, related := classifyLegacy(r, holders)
		switch verdict {
		case legacySkip:
			res.Skipped++
		case legacyReview:
			res.Review++
			if err := q.InsertLegacyReview(ctx, store.InsertLegacyReviewParams{
				LegacyID: r.in.legacyID, Reason: reason, Related: related,
			}); err != nil {
				return LegacyResult{}, fmt.Errorf("ghi mục cần xem: %w", err)
			}
		default:
			id, created, err := i.createLegacy(ctx, tx, q, r, tagIDs, a, now)
			if err != nil {
				return LegacyResult{}, err
			}
			if !created {
				res.Skipped++
				continue
			}
			res.OK++
			for _, p := range r.phones {
				if _, taken := holders[p]; !taken {
					holders[p] = holder{root: id, legacy: r.in.legacyID}
				}
			}
		}
	}
	slices.SortFunc(res.Errors, func(x, y ImportError) int { return x.Row - y.Row })
	return res, nil
}

type legacyVerdict int

const (
	legacyCreate legacyVerdict = iota
	legacySkip
	legacyReview
)

// classifyLegacy quyết định việc cho một khách theo chủ của các SĐT của nó (không bao giờ gộp):
//   - không SĐT nào có chủ → tạo;
//   - có chủ mà chính là hồ sơ của khách cũ này → bỏ qua (lượt khác vừa chuyển);
//   - chủ thuộc từ hai nhóm → cần xem phones_split;
//   - một nhóm chủ: đến từ khách cũ khác → duplicate_in_source; còn SĐT chưa có chủ → phone_not_added; không thì bỏ qua
//     (SĐT đã là của khách CRM, không ghi đè).
func classifyLegacy(r legacyRow, holders map[logging.Phone]holder) (legacyVerdict, string, []uuid.UUID) {
	var roots []uuid.UUID
	free, foreign := 0, false
	for _, p := range r.phones {
		h, ok := holders[p]
		switch {
		case !ok:
			free++
		case h.legacy == r.in.legacyID:
			return legacySkip, "", nil
		default:
			foreign = foreign || h.legacy != 0
			if !slices.Contains(roots, h.root) {
				roots = append(roots, h.root)
			}
		}
	}
	slices.SortFunc(roots, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
	switch {
	case len(roots) == 0:
		return legacyCreate, "", nil
	case len(roots) > 1:
		return legacyReview, LegacyReviewPhonesSplit, roots
	case foreign:
		return legacyReview, LegacyReviewDuplicate, roots
	case free > 0:
		return legacyReview, LegacyReviewPhoneNotAdded, roots
	}
	return legacySkip, "", nil
}

// createLegacy tạo khách của r (nhãn chưa có được tạo, nhớ trong tagIDs). created false: SĐT hay mã cũ vừa bị chiếm.
func (i *Importer) createLegacy(ctx context.Context, tx pgx.Tx, q *store.Queries, r legacyRow, tagIDs map[string]uuid.UUID,
	a Actor, now time.Time,
) (uuid.UUID, bool, error) {
	var missing []string
	for _, t := range r.in.tags {
		if _, ok := tagIDs[t]; !ok {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		got, err := ensureTags(ctx, q, []importRowInput{{tags: missing}})
		if err != nil {
			return uuid.Nil, false, err
		}
		for k, v := range got {
			tagIDs[k] = v
		}
	}
	return i.importRow(ctx, tx, r.in, tagIDs, a, now)
}

// importLegacy ghi phần riêng của khách chuyển từ hệ cũ trong savepoint của importRow: SĐT phụ (đã có chủ thì bỏ qua) và
// dòng đối chiếu mã cũ (mã đã có → errLegacyTaken, lùi cả khách). Đường Excel (legacyID 0) không làm gì.
func importLegacy(ctx context.Context, sp pgx.Tx, q *store.Queries, id uuid.UUID, in importRowInput, a Actor) error {
	if in.legacyID == 0 {
		return nil
	}
	for _, p := range in.morePhones {
		if _, _, _, err := insertIdentity(ctx, sp, id, IdentityPhone, "", string(p), SourceImport, a); err != nil {
			return err
		}
	}
	n, err := q.InsertLegacyCustomer(ctx, store.InsertLegacyCustomerParams{LegacyID: in.legacyID, CustomerID: id})
	if err != nil {
		return fmt.Errorf("ghi mã khách cũ: %w", err)
	}
	if n == 0 {
		return errLegacyTaken
	}
	return nil
}
