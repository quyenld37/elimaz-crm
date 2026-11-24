package promotion

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/promotion/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/settings"
	"elimaz.com.vn/crm/internal/platform/textnorm"
)

// Chiến dịch giảm giá web (kế hoạch M5, Task 452; QĐ33): CRM là nguồn sự thật, website đọc qua API web và tính giá
// hiển thị theo cùng luật (webcampaign_price.go); báo giá, tạo đơn web tính lại bằng ActiveForVariants.

// TopicWebCampaignChanged là sự kiện mỗi lần tạo, lưu, đổi trạng thái, xóa chiến dịch (aggregate web_campaign, Seq =
// revision mới); payload WebCampaignChanged.
const TopicWebCampaignChanged = "promotion.web_campaign.changed"

const (
	aggregateWebCampaign = "web_campaign"
	campaignSignalTopic  = "promotion.web_campaign"
	// SettingsKey là khóa cài đặt của chiến dịch web trong platform.settings (trần phần trăm).
	SettingsKey = "promotion.web_campaigns"
	// DefaultMaxPercent là trần phần trăm mặc định (PROMO_CAMPAIGN_MAX_PERCENT của website).
	DefaultMaxPercent = 50
	maxTargets        = 30
	maxReasonLen      = 300
	previewSampleMax  = 12
	insertAttempts    = 5
)

var (
	// ErrPreviewRequired báo: bật chiến dịch khi chưa xem trước đúng revision hiện tại, hoặc số mẫu, số biến thể đã xem
	// khác số tính lại lúc bật.
	ErrPreviewRequired = errors.New("promotion: cần xem trước chiến dịch")
	// ErrPercentTooHigh báo: phần trăm vượt trần cài đặt (mặc định 50).
	ErrPercentTooHigh = errors.New("promotion: phần trăm giảm vượt trần")
	// ErrCampaignRunning báo: sửa hay xóa chiến dịch đang bật; phải tạm dừng trước.
	ErrCampaignRunning = errors.New("promotion: chiến dịch đang bật")
	// errLinkKeySpace: sinh id trùng link_key insertAttempts lần liền (lỗi nội bộ).
	errLinkKeySpace = errors.New("promotion: sinh mã chiến dịch trùng quá số lần cho phép")
)

// WebCampaign là một chiến dịch giảm giá web. TargetIDs: mã mẫu (products) hay mã danh mục (categories), rỗng khi
// all. Preview là bản xem trước đã ghi (nil: chưa xem); chỉ có hiệu lực khi Preview.Revision = Revision.
type WebCampaign struct {
	ID, SellerID           uuid.UUID
	Name                   string
	Percent                int
	Scope                  string
	TargetIDs              []uuid.UUID
	AllowCoupon, Clearance bool
	StartsAt, EndsAt       time.Time
	State                  string
	LinkKey                string
	Revision               int
	Preview                *PreviewCounts
	CreatedBy              uuid.UUID
	CreatedAt, UpdatedAt   time.Time
}

// PreviewCounts là số mẫu, số biến thể sẽ đổi giá; Revision là revision của chiến dịch lúc xem.
type PreviewCounts struct {
	Revision         int
	Models, Variants int
}

// PreviewSample là một biến thể mẫu trong bản xem trước.
type PreviewSample struct {
	ProductID, VariantID uuid.UUID
	ProductName, SKU     string
	ListPrice, SalePrice int64
}

// Preview là kết quả xem trước: số đếm và tối đa 12 biến thể mẫu.
type Preview struct {
	PreviewCounts
	Samples []PreviewSample
}

// WebCampaignInput là nội dung nhân viên nhập (tạo, lưu).
type WebCampaignInput struct {
	Name                   string
	Percent                int
	Scope                  string
	TargetIDs              []uuid.UUID
	AllowCoupon, Clearance bool
	StartsAt, EndsAt       time.Time
}

// WebCampaignFilter lọc danh sách: Status là trạng thái hiển thị ở Now (rỗng: mọi); Q tìm trong tên; After con trỏ
// trang trước; Limit ngoài 1–100 lấy 50.
type WebCampaignFilter struct {
	Q, Status string
	Now       time.Time
	After     *uuid.UUID
	Limit     int
}

// CampaignChange là chiến dịch trước và sau một lần ghi (cho audit); After nil khi xóa.
type CampaignChange struct {
	Before *WebCampaign
	After  *WebCampaign
}

// CampaignTarget là một đích chọn được (mẫu: mã mẫu; danh mục: slug). ModelCount chỉ có ở danh mục: số mẫu đang bán,
// chưa xóa của người bán trong danh mục.
type CampaignTarget struct {
	ID         uuid.UUID
	Code, Name string
	ModelCount *int
}

// VariantRef là một biến thể cần giá chiến dịch: mẫu, danh mục của mẫu và giá niêm yết bên gọi đang dùng.
type VariantRef struct {
	VariantID, ProductID, CategoryID uuid.UUID
	ListPrice                        int64
}

// Applied là chiến dịch thắng cho một biến thể: đơn giá sau giảm và số giảm mỗi đơn vị.
type Applied struct {
	CampaignID          uuid.UUID
	Revision, Percent   int
	AllowCoupon         bool
	UnitPrice, Discount int64
}

// WebCampaignChanged là payload của TopicWebCampaignChanged.
type WebCampaignChanged struct {
	CampaignID uuid.UUID `json:"campaignId"`
	SellerID   uuid.UUID `json:"sellerId"`
	Revision   int       `json:"revision"`
	Deleted    bool      `json:"deleted,omitempty"`
}

type campaignSettings struct {
	MaxPercent int `json:"maxPercent"`
}

// MaxPercent đọc trần phần trăm của shop (khóa SettingsKey của platform.settings, trường maxPercent); chưa cài hay
// ngoài 1–99 → 50 (như campaignMaxPercent của website).
func MaxPercent(ctx context.Context, tx pgx.Tx) (int, error) {
	s, _, err := settings.Get(ctx, tx, SettingsKey, campaignSettings{MaxPercent: DefaultMaxPercent})
	if err != nil {
		return 0, fmt.Errorf("đọc trần phần trăm chiến dịch: %w", err)
	}
	if s.MaxPercent < 1 || s.MaxPercent > 99 {
		return DefaultMaxPercent, nil
	}
	return s.MaxPercent, nil
}

// AuditFields là phần của chiến dịch ghi vào audit.
func (c WebCampaign) AuditFields() map[string]any {
	ids := make([]string, 0, len(c.TargetIDs))
	for _, id := range c.TargetIDs {
		ids = append(ids, id.String())
	}
	return map[string]any{
		"name": c.Name, "percent": c.Percent, "scope": c.Scope, "targetIds": ids, "allowCoupon": c.AllowCoupon,
		"clearance": c.Clearance, "startsAt": c.StartsAt.UTC(), "endsAt": c.EndsAt.UTC(), "state": c.State,
		"revision": c.Revision,
	}
}

// CreateCampaign tạo chiến dịch nháp của người bán seller trong tx của bên gọi (tx tenant). Đầu vào sai, đích không có
// → ErrInvalidInput; phần trăm vượt trần → ErrPercentTooHigh.
func (w Writer) CreateCampaign(ctx context.Context, tx pgx.Tx, a Actor, seller uuid.UUID, in WebCampaignInput) (
	WebCampaign, error,
) {
	in, err := checkCampaignInput(ctx, tx, seller, in)
	if err != nil {
		return WebCampaign{}, err
	}
	q := store.New(tx)
	var row store.InsertWebCampaignRow
	inserted := false
	for range insertAttempts {
		row, err = q.InsertWebCampaign(ctx, store.InsertWebCampaignParams{
			ID: uuid.New(), SellerID: seller, Name: in.Name, Percent: int32(in.Percent), //nolint:gosec // G115: 1–99
			Scope: in.Scope, AllowCoupon: in.AllowCoupon, Clearance: in.Clearance, StartsAt: in.StartsAt,
			EndsAt: in.EndsAt, CreatedBy: a.UserID,
		})
		if err == nil {
			inserted = true
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return WebCampaign{}, fmt.Errorf("ghi chiến dịch: %w", err)
		}
	}
	if !inserted {
		return WebCampaign{}, errLinkKeySpace
	}
	c := campaignFrom(store.WebCampaignByIDRow(row))
	if err := replaceTargets(ctx, q, c.ID, in.Scope, in.TargetIDs); err != nil {
		return WebCampaign{}, err
	}
	c.TargetIDs = in.TargetIDs
	return c, w.campaignChanged(ctx, tx, a.TenantID, c, false)
}

// SaveCampaign lưu nội dung chiến dịch id khi revision còn khớp (khóa dòng trước, ADR-46 nhóm 10); state giữ nguyên,
// revision tăng 1 (bản xem trước cũ hết hiệu lực). Chiến dịch đang bật → ErrCampaignRunning (tạm dừng trước, như
// website); không có → ErrNotFound; revision cũ → ErrVersionConflict.
func (w Writer) SaveCampaign(ctx context.Context, tx pgx.Tx, a Actor, seller, id uuid.UUID, revision int,
	in WebCampaignInput,
) (CampaignChange, error) {
	q := store.New(tx)
	before, err := lockCampaign(ctx, q, seller, id, revision)
	if err != nil {
		return CampaignChange{}, err
	}
	if before.State == StateEnabled {
		return CampaignChange{}, ErrCampaignRunning
	}
	if in, err = checkCampaignInput(ctx, tx, seller, in); err != nil {
		return CampaignChange{}, err
	}
	row, err := q.UpdateWebCampaign(ctx, store.UpdateWebCampaignParams{
		Name: in.Name, Percent: int32(in.Percent), Scope: in.Scope, AllowCoupon: in.AllowCoupon, //nolint:gosec // G115: 1–99
		Clearance: in.Clearance, StartsAt: in.StartsAt, EndsAt: in.EndsAt, UpdatedBy: &a.UserID, ID: id,
		Revision: int32(revision), //nolint:gosec // G115: revision đã khớp dòng (int4)
	})
	if err != nil {
		return CampaignChange{}, fmt.Errorf("sửa chiến dịch: %w", err)
	}
	if err := replaceTargets(ctx, q, id, in.Scope, in.TargetIDs); err != nil {
		return CampaignChange{}, err
	}
	after := campaignFrom(store.WebCampaignByIDRow(row))
	after.TargetIDs = in.TargetIDs
	return CampaignChange{Before: &before, After: &after}, w.campaignChanged(ctx, tx, a.TenantID, after, false)
}

// PreviewCampaign tính bản xem trước của chiến dịch id ở đúng revision (khóa dòng, revision cũ → ErrVersionConflict)
// và ghi số đếm cho revision đó; không tăng revision, không phát sự kiện.
func (w Writer) PreviewCampaign(ctx context.Context, tx pgx.Tx, seller, id uuid.UUID, revision int) (Preview, error) {
	q := store.New(tx)
	c, err := lockCampaign(ctx, q, seller, id, revision)
	if err != nil {
		return Preview{}, err
	}
	p, err := computePreview(ctx, q, c)
	if err != nil {
		return Preview{}, err
	}
	if err := q.SetWebCampaignPreview(ctx, store.SetWebCampaignPreviewParams{
		PreviewModels: new(int32(p.Models)), PreviewVariants: new(int32(p.Variants)), //nolint:gosec // G115: số biến thể của shop
		ID: id, Revision: int32(revision), //nolint:gosec // G115: như SaveCampaign
	}); err != nil {
		return Preview{}, fmt.Errorf("ghi xem trước chiến dịch: %w", err)
	}
	return p, nil
}

// SetCampaignState bật (enabled) hay tạm dừng (paused) chiến dịch id; reason (trim) 1–300 ký tự bắt buộc. Bật: phần
// trăm vượt trần → ErrPercentTooHigh; chưa xem trước đúng revision, hay seen (số nhân viên đã thấy) khác bản đã ghi
// hoặc khác số tính lại lúc bật → ErrPreviewRequired.
func (w Writer) SetCampaignState(ctx context.Context, tx pgx.Tx, a Actor, seller, id uuid.UUID, revision int,
	state, reason string, seen PreviewCounts,
) (CampaignChange, error) {
	if state != StateEnabled && state != StatePaused {
		return CampaignChange{}, fmt.Errorf("trạng thái chiến dịch: %w", ErrInvalidInput)
	}
	if !validReason(reason) {
		return CampaignChange{}, fmt.Errorf("lý do: %w", ErrInvalidInput)
	}
	q := store.New(tx)
	before, err := lockCampaign(ctx, q, seller, id, revision)
	if err != nil {
		return CampaignChange{}, err
	}
	if state == StateEnabled {
		if err := checkEnable(ctx, tx, q, before, seen); err != nil {
			return CampaignChange{}, err
		}
	}
	row, err := q.SetWebCampaignState(ctx, store.SetWebCampaignStateParams{
		State: state, UpdatedBy: &a.UserID, ID: id, Revision: int32(revision), //nolint:gosec // G115: như SaveCampaign
	})
	if err != nil {
		return CampaignChange{}, fmt.Errorf("đổi trạng thái chiến dịch: %w", err)
	}
	after := campaignFrom(store.WebCampaignByIDRow(row))
	after.TargetIDs = before.TargetIDs
	return CampaignChange{Before: &before, After: &after}, w.campaignChanged(ctx, tx, a.TenantID, after, false)
}

// checkEnable kiểm trần phần trăm và bản xem trước khi bật c.
func checkEnable(ctx context.Context, tx pgx.Tx, q *store.Queries, c WebCampaign, seen PreviewCounts) error {
	maxPct, err := MaxPercent(ctx, tx)
	if err != nil {
		return err
	}
	if c.Percent > maxPct {
		return ErrPercentTooHigh
	}
	if c.Preview == nil || c.Preview.Revision != c.Revision || seen.Revision != c.Revision ||
		seen.Models != c.Preview.Models || seen.Variants != c.Preview.Variants {
		return ErrPreviewRequired
	}
	now, err := computePreview(ctx, q, c)
	if err != nil {
		return err
	}
	if now.Models != seen.Models || now.Variants != seen.Variants {
		return ErrPreviewRequired
	}
	return nil
}

// DeleteCampaign xóa mềm chiến dịch id (lý do 1–300 bắt buộc); đang bật → ErrCampaignRunning. Trả bản trước khi xóa.
func (w Writer) DeleteCampaign(ctx context.Context, tx pgx.Tx, a Actor, seller, id uuid.UUID, revision int,
	reason string,
) (WebCampaign, error) {
	if !validReason(reason) {
		return WebCampaign{}, fmt.Errorf("lý do: %w", ErrInvalidInput)
	}
	q := store.New(tx)
	before, err := lockCampaign(ctx, q, seller, id, revision)
	if err != nil {
		return WebCampaign{}, err
	}
	if before.State == StateEnabled {
		return WebCampaign{}, ErrCampaignRunning
	}
	rev, err := q.SoftDeleteWebCampaign(ctx, store.SoftDeleteWebCampaignParams{
		UpdatedBy: &a.UserID, ID: id, Revision: int32(revision), //nolint:gosec // G115: như SaveCampaign
	})
	if err != nil {
		return WebCampaign{}, fmt.Errorf("xóa chiến dịch: %w", err)
	}
	gone := before
	gone.Revision = int(rev)
	return before, w.campaignChanged(ctx, tx, a.TenantID, gone, true)
}

// lockCampaign khóa dòng chiến dịch id của seller (chưa xóa) kèm đích; không có → ErrNotFound, revision khác →
// ErrVersionConflict.
func lockCampaign(ctx context.Context, q *store.Queries, seller, id uuid.UUID, revision int) (WebCampaign, error) {
	row, err := q.WebCampaignByIDForUpdate(ctx, store.WebCampaignByIDForUpdateParams{ID: id, SellerID: seller})
	if errors.Is(err, pgx.ErrNoRows) {
		return WebCampaign{}, ErrNotFound
	}
	if err != nil {
		return WebCampaign{}, fmt.Errorf("khóa chiến dịch: %w", err)
	}
	c := campaignFrom(store.WebCampaignByIDRow(row))
	if c.Revision != revision {
		return WebCampaign{}, ErrVersionConflict
	}
	if err := attachTargets(ctx, q, []*WebCampaign{&c}); err != nil {
		return WebCampaign{}, err
	}
	return c, nil
}

// checkCampaignInput chuẩn hóa (trim tên, bỏ trùng không được phép) và kiểm in theo luật của website, trần phần
// trăm, đích có thật (mẫu chưa xóa của seller, danh mục có).
func checkCampaignInput(ctx context.Context, tx pgx.Tx, seller uuid.UUID, in WebCampaignInput) (WebCampaignInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	n := utf8.RuneCountInString(in.Name)
	switch {
	case n < 1 || n > maxNameLen || !utf8.ValidString(in.Name):
		return in, fmt.Errorf("tên chiến dịch: %w", ErrInvalidInput)
	case in.Percent < 1 || in.Percent > 99:
		return in, fmt.Errorf("phần trăm giảm: %w", ErrInvalidInput)
	case !in.StartsAt.Before(in.EndsAt):
		return in, fmt.Errorf("thời gian chiến dịch: %w", ErrInvalidInput)
	case !validTargets(in.Scope, in.TargetIDs):
		return in, fmt.Errorf("phạm vi chiến dịch: %w", ErrInvalidInput)
	}
	if in.TargetIDs == nil {
		in.TargetIDs = []uuid.UUID{}
	}
	maxPct, err := MaxPercent(ctx, tx)
	if err != nil {
		return in, err
	}
	if in.Percent > maxPct {
		return in, ErrPercentTooHigh
	}
	if len(in.TargetIDs) == 0 {
		return in, nil
	}
	q := store.New(tx)
	var found []uuid.UUID
	if in.Scope == ScopeProducts {
		found, err = q.ExistingProducts(ctx, store.ExistingProductsParams{SellerID: seller, Ids: in.TargetIDs})
	} else {
		found, err = q.ExistingCategories(ctx, in.TargetIDs)
	}
	if err != nil {
		return in, fmt.Errorf("kiểm đích chiến dịch: %w", err)
	}
	if len(found) != len(in.TargetIDs) {
		return in, fmt.Errorf("đích chiến dịch không có: %w", ErrInvalidInput)
	}
	return in, nil
}

// validTargets: all thì không đích; products, categories thì 1–30 đích không trùng, không rỗng.
func validTargets(scope string, ids []uuid.UUID) bool {
	switch scope {
	case ScopeAll:
		return len(ids) == 0
	case ScopeProducts, ScopeCategories:
		if len(ids) < 1 || len(ids) > maxTargets || slices.Contains(ids, uuid.Nil) {
			return false
		}
		s := slices.Clone(ids)
		slices.SortFunc(s, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		return len(slices.Compact(s)) == len(ids)
	}
	return false
}

func validReason(r string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(r))
	return n >= 1 && n <= maxReasonLen
}

// replaceTargets thay đích của chiến dịch id (đã khóa dòng): xóa hết rồi chèn.
func replaceTargets(ctx context.Context, q *store.Queries, id uuid.UUID, scope string, ids []uuid.UUID) error {
	if err := q.DeleteWebCampaignTargets(ctx, id); err != nil {
		return fmt.Errorf("xóa đích chiến dịch: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	kind := "product"
	if scope == ScopeCategories {
		kind = "category"
	}
	if err := q.InsertWebCampaignTargets(ctx, store.InsertWebCampaignTargetsParams{
		CampaignID: id, TargetKind: kind, TargetIds: ids,
	}); err != nil {
		return fmt.Errorf("ghi đích chiến dịch: %w", err)
	}
	return nil
}

// computePreview tính bản xem trước của c trên biến thể đang bán của người bán: biến thể có giá sau giảm áp được.
func computePreview(ctx context.Context, q *store.Queries, c WebCampaign) (Preview, error) {
	p := store.CoveredVariantsParams{SellerID: c.SellerID, Scope: c.Scope, ProductIds: []uuid.UUID{}, CategoryIds: []uuid.UUID{}}
	switch c.Scope {
	case ScopeProducts:
		p.ProductIds = c.TargetIDs
	case ScopeCategories:
		p.CategoryIds = c.TargetIDs
	}
	rows, err := q.CoveredVariants(ctx, p)
	if err != nil {
		return Preview{}, fmt.Errorf("đếm biến thể của chiến dịch: %w", err)
	}
	out := Preview{PreviewCounts: PreviewCounts{Revision: c.Revision}, Samples: []PreviewSample{}}
	models := map[uuid.UUID]bool{}
	for _, r := range rows {
		sale, ok := CampaignPrice(r.Price, c.Percent)
		if !ok {
			continue
		}
		models[r.ProductID] = true
		out.Variants++
		if len(out.Samples) < previewSampleMax {
			out.Samples = append(out.Samples, PreviewSample{
				ProductID: r.ProductID, VariantID: r.VariantID, ProductName: r.ProductName, SKU: r.Sku,
				ListPrice: r.Price, SalePrice: sale,
			})
		}
	}
	out.Models = len(models)
	return out, nil
}

// campaignChanged phát TopicWebCampaignChanged (Seq = revision) và báo hiệu cho màn hình nhân viên, trong tx.
func (w Writer) campaignChanged(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, c WebCampaign, deleted bool) error {
	if w.Events == nil || w.Queue == nil {
		return fmt.Errorf("phát %s: Writer thiếu Events, Queue: %w", TopicWebCampaignChanged, events.ErrInvalidEvent)
	}
	if _, _, err := w.Events.Emit(ctx, tx, w.Queue, events.Event{
		TenantID: tenant, AggregateType: aggregateWebCampaign, AggregateID: c.ID.String(), Seq: int64(c.Revision),
		Topic:   TopicWebCampaignChanged,
		Payload: WebCampaignChanged{CampaignID: c.ID, SellerID: c.SellerID, Revision: c.Revision, Deleted: deleted},
	}); err != nil {
		return fmt.Errorf("phát %s: %w", TopicWebCampaignChanged, err)
	}
	if w.Realtime == nil {
		return nil
	}
	if err := w.Realtime.Publish(ctx, tx, realtime.Signal{
		TenantID: tenant, Topic: campaignSignalTopic, Requires: permRead,
		Entity: realtime.Entity{Type: aggregateWebCampaign, ID: c.ID.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu chiến dịch: %w", err)
	}
	return nil
}

// GetWebCampaign đọc chiến dịch id (chưa xóa) của seller kèm đích; không có → ErrNotFound.
func GetWebCampaign(ctx context.Context, tx pgx.Tx, seller, id uuid.UUID) (WebCampaign, error) {
	q := store.New(tx)
	row, err := q.WebCampaignByID(ctx, store.WebCampaignByIDParams{ID: id, SellerID: seller})
	if errors.Is(err, pgx.ErrNoRows) {
		return WebCampaign{}, ErrNotFound
	}
	if err != nil {
		return WebCampaign{}, fmt.Errorf("đọc chiến dịch: %w", err)
	}
	c := campaignFrom(row)
	return c, attachTargets(ctx, q, []*WebCampaign{&c})
}

// ListWebCampaigns trả một trang chiến dịch của seller theo f (mới nhất trước) và con trỏ trang sau (nil: hết).
func ListWebCampaigns(ctx context.Context, tx pgx.Tx, seller uuid.UUID, f WebCampaignFilter) ([]WebCampaign, *uuid.UUID, error) {
	limit := pageLimit(f.Limit)
	p := store.ListWebCampaignsParams{SellerID: seller, Now: f.Now, After: f.After, RowLimit: int32(limit + 1)} //nolint:gosec // G115: ≤ 101
	if q := strings.TrimSpace(f.Q); q != "" {
		p.Q.String, p.Q.Valid = q, true
	}
	if f.Status != "" {
		p.Status.String, p.Status.Valid = f.Status, true
	}
	q := store.New(tx)
	rows, err := q.ListWebCampaigns(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách chiến dịch: %w", err)
	}
	out := make([]WebCampaign, 0, min(len(rows), limit))
	for _, r := range rows[:min(len(rows), limit)] {
		out = append(out, campaignFrom(store.WebCampaignByIDRow(r)))
	}
	if err := attachTargets(ctx, q, pointers(out)); err != nil {
		return nil, nil, err
	}
	if len(rows) > limit {
		next := out[len(out)-1].ID
		return out, &next, nil
	}
	return out, nil, nil
}

// ListCampaignTargets trả một trang đích chọn được: kind "product" (mẫu chưa xóa của seller; q theo mã hay tên không
// dấu) hay "category" (q theo tên, slug); theo id, con trỏ là id cuối trang trước.
func ListCampaignTargets(ctx context.Context, tx pgx.Tx, seller uuid.UUID, kind, q string, after *uuid.UUID, limit int) (
	[]CampaignTarget, *uuid.UUID, error,
) {
	limit = pageLimit(limit)
	q = strings.TrimSpace(q)
	st := store.New(tx)
	var out []CampaignTarget
	switch kind {
	case "product":
		p := store.TargetProductsParams{SellerID: seller, After: after, RowLimit: int32(limit + 1)} //nolint:gosec // G115: ≤ 101
		if q != "" {
			p.Q.String, p.Q.Valid = q, true
			p.QFolded.String, p.QFolded.Valid = textnorm.Fold(q), true
		}
		rows, err := st.TargetProducts(ctx, p)
		if err != nil {
			return nil, nil, fmt.Errorf("đọc mẫu chọn làm đích: %w", err)
		}
		for _, r := range rows {
			out = append(out, CampaignTarget{ID: r.ID, Code: r.Code, Name: r.Name})
		}
	case "category":
		p := store.TargetCategoriesParams{SellerID: seller, After: after, RowLimit: int32(limit + 1)} //nolint:gosec // G115: ≤ 101
		if q != "" {
			p.Q.String, p.Q.Valid = q, true
		}
		rows, err := st.TargetCategories(ctx, p)
		if err != nil {
			return nil, nil, fmt.Errorf("đọc danh mục chọn làm đích: %w", err)
		}
		for _, r := range rows {
			out = append(out, categoryTarget(r.ID, r.Code, r.Name, r.ModelCount))
		}
	default:
		return nil, nil, fmt.Errorf("loại đích: %w", ErrInvalidInput)
	}
	if len(out) > limit {
		next := out[limit-1].ID
		return out[:limit], &next, nil
	}
	if out == nil {
		out = []CampaignTarget{}
	}
	return out, nil, nil
}

// CampaignTargetsByIDs trả tên, mã (và số mẫu của danh mục) của các đích ids của chiến dịch phạm vi scope, theo thứ tự
// ids (màn sửa chiến dịch, Task 487). Đích không còn (mẫu đã xóa, danh mục đã bỏ) không có trong kết quả; all → rỗng.
func CampaignTargetsByIDs(ctx context.Context, tx pgx.Tx, seller uuid.UUID, scope string, ids []uuid.UUID) (
	[]CampaignTarget, error,
) {
	found := make(map[uuid.UUID]CampaignTarget, len(ids))
	st := store.New(tx)
	switch {
	case len(ids) == 0 || scope == ScopeAll:
	case scope == ScopeProducts:
		rows, err := st.TargetProductsByIDs(ctx, store.TargetProductsByIDsParams{SellerID: seller, Ids: ids})
		if err != nil {
			return nil, fmt.Errorf("đọc mẫu đích của chiến dịch: %w", err)
		}
		for _, r := range rows {
			found[r.ID] = CampaignTarget{ID: r.ID, Code: r.Code, Name: r.Name}
		}
	case scope == ScopeCategories:
		rows, err := st.TargetCategoriesByIDs(ctx, store.TargetCategoriesByIDsParams{SellerID: seller, Ids: ids})
		if err != nil {
			return nil, fmt.Errorf("đọc danh mục đích của chiến dịch: %w", err)
		}
		for _, r := range rows {
			found[r.ID] = categoryTarget(r.ID, r.Code, r.Name, r.ModelCount)
		}
	default:
		return nil, fmt.Errorf("phạm vi chiến dịch: %w", ErrInvalidInput)
	}
	out := make([]CampaignTarget, 0, len(found))
	for _, id := range ids {
		if t, ok := found[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func categoryTarget(id uuid.UUID, code, name string, models int32) CampaignTarget {
	n := int(models)
	return CampaignTarget{ID: id, Code: code, Name: name, ModelCount: &n}
}

// WebCampaigns trả chiến dịch đã bật, chưa xóa, chưa hết giờ ở now của mọi người bán (đang chạy hay đã hẹn giờ) kèm
// đích, theo id: danh sách website đọc.
func WebCampaigns(ctx context.Context, tx pgx.Tx, now time.Time) ([]WebCampaign, error) {
	return liveCampaigns(ctx, tx, nil, now)
}

// WebCampaignByLinkKey đọc chiến dịch chưa xóa có link key key (trang /uu-dai của website) kèm đích; không có hay còn
// nháp → ErrNotFound.
func WebCampaignByLinkKey(ctx context.Context, tx pgx.Tx, key string) (WebCampaign, error) {
	q := store.New(tx)
	row, err := q.WebCampaignByLinkKey(ctx, strings.ToLower(key))
	if errors.Is(err, pgx.ErrNoRows) {
		return WebCampaign{}, ErrNotFound
	}
	if err != nil {
		return WebCampaign{}, fmt.Errorf("đọc chiến dịch theo liên kết: %w", err)
	}
	c := campaignFrom(store.WebCampaignByIDRow(row))
	if c.State == StateDraft {
		return WebCampaign{}, ErrNotFound
	}
	return c, attachTargets(ctx, q, []*WebCampaign{&c})
}

// ActiveForVariants trả, cho mỗi biến thể của vs có chiến dịch của seller đang chạy ở now phủ nó và cho giá áp được,
// chiến dịch thắng (BestCampaign trên giá niêm yết ListPrice của bên gọi). Biến thể không có chiến dịch không có trong
// map. Dùng ở danh mục web, báo giá, tạo đơn web (M5-07, 09, 10).
func ActiveForVariants(ctx context.Context, tx pgx.Tx, seller uuid.UUID, vs []VariantRef, now time.Time) (
	map[uuid.UUID]Applied, error,
) {
	out := map[uuid.UUID]Applied{}
	if len(vs) == 0 {
		return out, nil
	}
	live, err := liveCampaigns(ctx, tx, &seller, now)
	if err != nil {
		return nil, err
	}
	running := live[:0]
	for _, c := range live {
		if Running(c, now) {
			running = append(running, c)
		}
	}
	if len(running) == 0 {
		return out, nil
	}
	for _, v := range vs {
		var cands []WebCampaign
		for _, c := range running {
			if CampaignCovers(c, v) {
				cands = append(cands, c)
			}
		}
		if a, ok := BestCampaign(cands, v.ListPrice); ok {
			out[v.VariantID] = a
		}
	}
	return out, nil
}

func liveCampaigns(ctx context.Context, tx pgx.Tx, seller *uuid.UUID, now time.Time) ([]WebCampaign, error) {
	q := store.New(tx)
	rows, err := q.LiveWebCampaigns(ctx, store.LiveWebCampaignsParams{Now: now, SellerID: seller})
	if err != nil {
		return nil, fmt.Errorf("đọc chiến dịch đang bật: %w", err)
	}
	out := make([]WebCampaign, 0, len(rows))
	for _, r := range rows {
		out = append(out, campaignFrom(store.WebCampaignByIDRow(r)))
	}
	return out, attachTargets(ctx, q, pointers(out))
}

// attachTargets đọc đích của các chiến dịch cs trong một câu.
func attachTargets(ctx context.Context, q *store.Queries, cs []*WebCampaign) error {
	if len(cs) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(cs))
	byID := make(map[uuid.UUID]*WebCampaign, len(cs))
	for _, c := range cs {
		ids = append(ids, c.ID)
		byID[c.ID] = c
		c.TargetIDs = []uuid.UUID{}
	}
	rows, err := q.WebCampaignTargets(ctx, ids)
	if err != nil {
		return fmt.Errorf("đọc đích chiến dịch: %w", err)
	}
	for _, r := range rows {
		c := byID[r.CampaignID]
		c.TargetIDs = append(c.TargetIDs, r.TargetID)
	}
	return nil
}

func pointers(cs []WebCampaign) []*WebCampaign {
	out := make([]*WebCampaign, 0, len(cs))
	for i := range cs {
		out = append(out, &cs[i])
	}
	return out
}

func campaignFrom(r store.WebCampaignByIDRow) WebCampaign {
	c := WebCampaign{
		ID: r.ID, SellerID: r.SellerID, Name: r.Name, Percent: int(r.Percent), Scope: r.Scope, AllowCoupon: r.AllowCoupon,
		Clearance: r.Clearance, StartsAt: r.StartsAt, EndsAt: r.EndsAt, State: r.State, LinkKey: r.LinkKey,
		Revision: int(r.Revision), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.PreviewRevision != nil && r.PreviewModels != nil && r.PreviewVariants != nil {
		c.Preview = &PreviewCounts{
			Revision: int(*r.PreviewRevision), Models: int(*r.PreviewModels), Variants: int(*r.PreviewVariants),
		}
	}
	return c
}
