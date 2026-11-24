package promotion

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/promotion/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Báo hiệu realtime khi mã, đợt mã đổi (Q-T71-6): chủ đề đặt như catalog.product, inventory.balance; quyền đọc của
// promotion (identity.Permission; promotion không nhập identity).
const (
	signalTopic = "promotion.coupon"
	permRead    = "promotion.read"
)

// maxMoney: trần tiền của hợp đồng API và CHECK của bảng (2^53 − 1).
const maxMoney = 9007199254740991

// maxNameLen: độ dài tối đa của tên mã, tên đợt (CHECK của bảng).
const maxNameLen = 120

// Phân trang danh sách mã, lượt dùng (hợp đồng: Limit 1–100, mặc định 50).
const (
	defaultLimit = 50
	maxLimit     = 100
)

var codePattern = regexp.MustCompile(`^[A-Za-z0-9-]{3,32}$`)

// errCodeSpace: sinh mã trùng mã có sẵn codeAttempts lần liền (nguồn ngẫu nhiên hỏng hay bộ sinh sai); lỗi nội bộ.
var errCodeSpace = errors.New("promotion: sinh mã trùng quá số lần cho phép")

// Actor là nhân viên đang sửa mã: tenant và mã nhân viên (promotion không nhập identity).
type Actor struct {
	TenantID uuid.UUID
	UserID   uuid.UUID
}

// Writer là đường ghi mã, đợt mã và lượt dùng mã. Realtime nil → không báo hiệu (báo hiệu được phép mất); Events,
// Queue bắt buộc cho Redeem (phát coupon.used), không cần cho việc khác.
type Writer struct {
	Realtime realtime.Bus
	Events   *events.Registry
	Queue    queue.JobQueue

	newCode func(prefix string) (string, error) // nil → NewCode; test cắm bộ sinh trùng
}

// CouponInput là điều khoản của mã mới. Code rỗng → máy chủ sinh mã 8 ký tự. BoundCustomerID là khách bất kỳ (lưu hồ sơ
// gốc), BoundPhone dạng nào cũng được (lưu +84…).
type CouponInput struct {
	Code             string
	Name             string
	DiscountKind     string
	Value            int64
	MaxDiscount      *int64
	MinSubtotal      int64
	Channels         []string
	MaxUses          *int32
	PerCustomerLimit *int32
	ReleaseOnCancel  bool
	ValidFrom        time.Time
	ValidTo          time.Time
	BoundCustomerID  uuid.UUID
	BoundPhone       string
}

// CouponPatch là phần sửa của một mã: con trỏ nil, Channels nil là không đổi; Channels rỗng khác nil là mọi kênh.
// Không gỡ được MaxDiscount, MaxUses, PerCustomerLimit về rỗng (hợp đồng chưa có cách gửi).
type CouponPatch struct {
	Name             *string
	DiscountKind     *string
	Value            *int64
	MaxDiscount      *int64
	MinSubtotal      *int64
	Channels         []string
	MaxUses          *int32
	PerCustomerLimit *int32
	ReleaseOnCancel  *bool
	ValidFrom        *time.Time
	ValidTo          *time.Time
	Status           *string
}

// CouponChange là mã trước và sau khi sửa (cho audit).
type CouponChange struct {
	Before Coupon
	After  Coupon
}

// CouponFilter lọc danh sách mã. Q tìm trong mã hoặc tên (không phân biệt hoa thường); Status, BatchID rỗng là mọi;
// After là con trỏ của trang trước; Limit ngoài 1–100 lấy 50.
type CouponFilter struct {
	Q       string
	Status  string
	BatchID uuid.UUID
	After   *uuid.UUID
	Limit   int
}

// RedemptionRow là một lượt dùng mã trong danh sách lượt (không có SĐT, khách; mã đơn do bên gọi đọc từ order).
type RedemptionRow struct {
	ID         uuid.UUID
	OrderID    uuid.UUID
	Discount   int64
	Status     string
	CreatedAt  time.Time
	ReleasedAt *time.Time
}

// RemainingUses trả số lượt còn của mã (nil: không giới hạn).
func (c Coupon) RemainingUses() *int32 {
	if c.MaxUses == nil {
		return nil
	}
	rem := max(*c.MaxUses-c.UsedCount, 0)
	return &rem
}

// AuditFields là phần của mã ghi vào audit: không có SĐT, khách gắn mã, chỉ có bound (có gắn hay không).
func (c Coupon) AuditFields() map[string]any {
	m := map[string]any{
		"code": c.Code, "name": c.Name, "discountKind": c.DiscountKind, "value": c.Value, "minSubtotal": c.MinSubtotal,
		"channels": c.Channels, "releaseOnCancel": c.ReleaseOnCancel, "validFrom": c.ValidFrom.UTC(),
		"validTo": c.ValidTo.UTC(), "status": c.Status, "usedCount": c.UsedCount, "version": c.Version,
		"bound": c.Bound(), "maxDiscount": nil, "maxUses": nil, "perCustomerLimit": nil, "batchId": nil,
	}
	if c.MaxDiscount != nil {
		m["maxDiscount"] = *c.MaxDiscount
	}
	if c.MaxUses != nil {
		m["maxUses"] = *c.MaxUses
	}
	if c.PerCustomerLimit != nil {
		m["perCustomerLimit"] = *c.PerCustomerLimit
	}
	if c.BatchID != uuid.Nil {
		m["batchId"] = c.BatchID.String()
	}
	return m
}

// Bound cho biết mã gắn với một khách hoặc một SĐT.
func (c Coupon) Bound() bool { return c.BoundCustomerID != uuid.Nil || c.BoundPhone != "" }

// CreateCoupon tạo mã in trong tx của bên gọi (tx tenant). Mã người nhập trùng mã có sẵn (không phân biệt hoa thường)
// → ErrCodeTaken, không làm hỏng tx; mã để trống thì sinh, trùng thì sinh lại tối đa 5 lần. Đầu vào sai →
// ErrInvalidInput (khách gắn mã không có cũng vậy); SĐT sai → customer.ErrInvalidPhone.
func (w Writer) CreateCoupon(ctx context.Context, tx pgx.Tx, a Actor, in CouponInput) (Coupon, error) {
	c := Coupon{
		Name: strings.TrimSpace(in.Name), DiscountKind: in.DiscountKind, Value: in.Value, MaxDiscount: in.MaxDiscount,
		MinSubtotal: in.MinSubtotal, Channels: channelsOrEmpty(in.Channels), MaxUses: in.MaxUses,
		PerCustomerLimit: in.PerCustomerLimit, ReleaseOnCancel: in.ReleaseOnCancel, ValidFrom: in.ValidFrom,
		ValidTo: in.ValidTo, Status: StatusActive,
	}
	if err := validateTerms(c); err != nil {
		return Coupon{}, err
	}
	if in.Code != "" && !codePattern.MatchString(in.Code) {
		return Coupon{}, fmt.Errorf("mã giảm giá: %w", ErrInvalidInput)
	}
	p := store.InsertCouponParams{
		Name: c.Name, DiscountKind: c.DiscountKind, Value: c.Value, MaxDiscount: c.MaxDiscount,
		MinSubtotal: c.MinSubtotal, Channels: c.Channels, MaxUses: c.MaxUses, PerCustomerLimit: c.PerCustomerLimit,
		ReleaseOnCancel: c.ReleaseOnCancel, ValidFrom: c.ValidFrom, ValidTo: c.ValidTo, CreatedBy: a.UserID,
	}
	if in.BoundPhone != "" {
		phone, err := customer.NormalizePhone(in.BoundPhone)
		if err != nil {
			return Coupon{}, fmt.Errorf("SĐT gắn mã: %w", err)
		}
		s := string(phone)
		p.BoundPhone = &s
	}
	if in.BoundCustomerID != uuid.Nil {
		root, err := CustomerRoot(ctx, tx, in.BoundCustomerID)
		if err != nil {
			return Coupon{}, err
		}
		p.BoundCustomerID = &root
	}
	row, err := w.insertCoupon(ctx, store.New(tx), in.Code, p)
	if err != nil {
		return Coupon{}, err
	}
	out := couponFrom(store.CouponByCodeRow(row))
	return out, w.signal(ctx, tx, a.TenantID, "coupon", out.ID)
}

// insertCoupon chèn mã code (rỗng → sinh, tối đa codeAttempts lần khi trùng).
func (w Writer) insertCoupon(ctx context.Context, q *store.Queries, code string, p store.InsertCouponParams) (
	store.InsertCouponRow, error,
) {
	attempts := codeAttempts
	if code != "" {
		attempts = 1
	}
	for range attempts {
		p.Code = code
		if code == "" {
			gen, err := w.generate("")
			if err != nil {
				return store.InsertCouponRow{}, err
			}
			p.Code = gen
		}
		row, err := q.InsertCoupon(ctx, p)
		if err == nil {
			return row, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return store.InsertCouponRow{}, fmt.Errorf("ghi mã giảm giá: %w", err)
		}
	}
	if code != "" {
		return store.InsertCouponRow{}, ErrCodeTaken
	}
	return store.InsertCouponRow{}, errCodeSpace
}

// UpdateCoupon sửa mã id khi version còn khớp: khóa dòng mã (ADR-46 nhóm 10, trước mọi bảng lượt), so version, áp phần
// sửa, kiểm điều khoản. Mã đã có lượt dùng (used_count > 0 hay có dòng lượt kể cả released): chỉ được đổi tên, hạn cuối,
// số lượt tối đa, trạng thái; trường khác có giá trị khác giá trị hiện tại → ErrCouponLockedAfterUse (Q-T71-4). Số lượt
// tối đa dưới số đã dùng → ErrMaxUsesBelowUsed. Không có → ErrNotFound; version cũ → ErrVersionConflict.
func (w Writer) UpdateCoupon(ctx context.Context, tx pgx.Tx, a Actor, id uuid.UUID, version int64, p CouponPatch) (
	CouponChange, error,
) {
	q := store.New(tx)
	row, err := q.CouponByIDForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return CouponChange{}, ErrNotFound
	}
	if err != nil {
		return CouponChange{}, fmt.Errorf("khóa mã giảm giá: %w", err)
	}
	before := couponFrom(store.CouponByCodeRow(row))
	if before.Version != version {
		return CouponChange{}, ErrVersionConflict
	}
	after := applyPatch(before, p)
	if err := validateTerms(after); err != nil {
		return CouponChange{}, err
	}
	used := before.UsedCount > 0
	if !used {
		if used, err = q.HasRedemptions(ctx, id); err != nil {
			return CouponChange{}, fmt.Errorf("đọc lượt dùng của mã: %w", err)
		}
	}
	if used && lockedChanged(before, after) {
		return CouponChange{}, ErrCouponLockedAfterUse
	}
	if after.MaxUses != nil && *after.MaxUses < before.UsedCount {
		return CouponChange{}, ErrMaxUsesBelowUsed
	}
	updated, err := q.UpdateCoupon(ctx, store.UpdateCouponParams{
		Name: after.Name, DiscountKind: after.DiscountKind, Value: after.Value, MaxDiscount: after.MaxDiscount,
		MinSubtotal: after.MinSubtotal, Channels: after.Channels, MaxUses: after.MaxUses,
		PerCustomerLimit: after.PerCustomerLimit, ReleaseOnCancel: after.ReleaseOnCancel, ValidFrom: after.ValidFrom,
		ValidTo: after.ValidTo, Status: after.Status, ID: id, Version: version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return CouponChange{}, ErrVersionConflict // không xảy ra khi giữ khóa dòng
	}
	if err != nil {
		return CouponChange{}, fmt.Errorf("sửa mã giảm giá: %w", err)
	}
	ch := CouponChange{Before: before, After: couponFrom(store.CouponByCodeRow(updated))}
	return ch, w.signal(ctx, tx, a.TenantID, "coupon", id)
}

// applyPatch trả c sau khi áp p (không kiểm).
func applyPatch(c Coupon, p CouponPatch) Coupon {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	if p.Name != nil {
		c.Name = strings.TrimSpace(*p.Name)
	}
	set(&c.DiscountKind, p.DiscountKind)
	set(&c.Status, p.Status)
	if p.Value != nil {
		c.Value = *p.Value
	}
	if p.MaxDiscount != nil {
		c.MaxDiscount = p.MaxDiscount
	}
	if p.MinSubtotal != nil {
		c.MinSubtotal = *p.MinSubtotal
	}
	if p.Channels != nil {
		c.Channels = p.Channels
	}
	if p.MaxUses != nil {
		c.MaxUses = p.MaxUses
	}
	if p.PerCustomerLimit != nil {
		c.PerCustomerLimit = p.PerCustomerLimit
	}
	if p.ReleaseOnCancel != nil {
		c.ReleaseOnCancel = *p.ReleaseOnCancel
	}
	if p.ValidFrom != nil {
		c.ValidFrom = *p.ValidFrom
	}
	if p.ValidTo != nil {
		c.ValidTo = *p.ValidTo
	}
	return c
}

// lockedChanged: một trường bị khóa sau khi dùng (mọi trường trừ tên, hạn cuối, số lượt tối đa, trạng thái) có giá trị
// khác. Kênh so như tập hợp.
func lockedChanged(a, b Coupon) bool {
	return a.DiscountKind != b.DiscountKind || a.Value != b.Value || !eqPtr(a.MaxDiscount, b.MaxDiscount) ||
		a.MinSubtotal != b.MinSubtotal || !sameSet(a.Channels, b.Channels) ||
		!eqPtr(a.PerCustomerLimit, b.PerCustomerLimit) || a.ReleaseOnCancel != b.ReleaseOnCancel ||
		!a.ValidFrom.Equal(b.ValidFrom)
}

func eqPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(slices.Compact(x), slices.Compact(y))
}

// validateTerms kiểm điều khoản của mã theo CHECK của bảng (trước khi ghi, để trả ErrInvalidInput thay vì lỗi 500).
func validateTerms(c Coupon) error {
	switch {
	case !validName(c.Name):
		return fmt.Errorf("tên mã: %w", ErrInvalidInput)
	case c.DiscountKind == KindPercent && (c.Value < 1 || c.Value > percentScale),
		c.DiscountKind == KindFixed && (c.Value < 1 || c.Value > maxMoney),
		c.DiscountKind != KindPercent && c.DiscountKind != KindFixed:
		return fmt.Errorf("loại, giá trị giảm: %w", ErrInvalidInput)
	case c.MaxDiscount != nil && (*c.MaxDiscount < 1 || *c.MaxDiscount > maxMoney),
		c.MinSubtotal < 0 || c.MinSubtotal > maxMoney:
		return fmt.Errorf("trần giảm, tạm tính tối thiểu: %w", ErrInvalidInput)
	case !validChannels(c.Channels):
		return fmt.Errorf("kênh: %w", ErrInvalidInput)
	case c.MaxUses != nil && *c.MaxUses < 1, c.PerCustomerLimit != nil && *c.PerCustomerLimit < 1:
		return fmt.Errorf("số lượt: %w", ErrInvalidInput)
	case !c.ValidTo.After(c.ValidFrom):
		return fmt.Errorf("thời hạn: %w", ErrInvalidInput)
	case c.Status != StatusActive && c.Status != StatusDisabled:
		return fmt.Errorf("trạng thái: %w", ErrInvalidInput)
	}
	return nil
}

func validName(s string) bool {
	n := utf8.RuneCountInString(s)
	return n >= 1 && n <= maxNameLen && utf8.ValidString(s)
}

func validChannels(cs []string) bool {
	for _, c := range cs {
		if c != "facebook" && c != "web" && c != "manual" {
			return false
		}
	}
	return true
}

func channelsOrEmpty(cs []string) []string {
	if cs == nil {
		return []string{}
	}
	return cs
}

// generate sinh một mã bằng bộ sinh của w.
func (w Writer) generate(prefix string) (string, error) {
	if w.newCode != nil {
		return w.newCode(prefix)
	}
	return NewCode(prefix)
}

// signal báo hiệu promotion.coupon cho mã hay đợt mã (entity "coupon" / "coupon_batch") trong tx; chỉ đi khi commit.
// Realtime nil → bỏ qua (báo hiệu được phép mất).
func (w Writer) signal(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, entity string, id uuid.UUID) error {
	var err error
	if w.Realtime != nil {
		err = w.Realtime.Publish(ctx, tx, realtime.Signal{
			TenantID: tenantID, Topic: signalTopic, Requires: permRead, Entity: realtime.Entity{Type: entity, ID: id.String()},
		})
	}
	if err != nil {
		return fmt.Errorf("báo hiệu mã giảm giá: %w", err)
	}
	return nil
}

// GetCoupon đọc mã id; không có → ErrNotFound.
func GetCoupon(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Coupon, error) {
	row, err := store.New(tx).CouponByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Coupon{}, ErrNotFound
	}
	if err != nil {
		return Coupon{}, fmt.Errorf("đọc mã giảm giá: %w", err)
	}
	return couponFrom(store.CouponByCodeRow(row)), nil
}

// ListCoupons trả một trang mã theo f, mới nhất trước, và con trỏ trang sau (nil: hết).
func ListCoupons(ctx context.Context, tx pgx.Tx, f CouponFilter) ([]Coupon, *uuid.UUID, error) {
	limit := pageLimit(f.Limit)
	p := store.ListCouponsParams{After: f.After, RowLimit: int32(limit + 1)} //nolint:gosec // G115: limit ≤ 100
	if f.Q != "" {
		p.Q.String, p.Q.Valid = f.Q, true
	}
	if f.Status != "" {
		p.Status.String, p.Status.Valid = f.Status, true
	}
	if f.BatchID != uuid.Nil {
		p.BatchID = &f.BatchID
	}
	rows, err := store.New(tx).ListCoupons(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách mã giảm giá: %w", err)
	}
	out := make([]Coupon, 0, min(len(rows), limit))
	for _, r := range rows[:min(len(rows), limit)] {
		out = append(out, couponFrom(store.CouponByCodeRow(r)))
	}
	if len(rows) > limit {
		next := out[len(out)-1].ID
		return out, &next, nil
	}
	return out, nil, nil
}

// ListRedemptions trả một trang lượt dùng của mã couponID, mới nhất trước (after: con trỏ trang trước); mã không có →
// ErrNotFound.
func ListRedemptions(ctx context.Context, tx pgx.Tx, couponID uuid.UUID, after *uuid.UUID, limit int) (
	[]RedemptionRow, *uuid.UUID, error,
) {
	if _, err := GetCoupon(ctx, tx, couponID); err != nil {
		return nil, nil, err
	}
	limit = pageLimit(limit)
	rows, err := store.New(tx).ListRedemptions(ctx, store.ListRedemptionsParams{
		CouponID: couponID, After: after, RowLimit: int32(limit + 1), //nolint:gosec // G115: limit ≤ 100
	})
	if err != nil {
		return nil, nil, fmt.Errorf("đọc lượt dùng mã: %w", err)
	}
	out := make([]RedemptionRow, 0, min(len(rows), limit))
	for _, r := range rows[:min(len(rows), limit)] {
		row := RedemptionRow{
			ID: r.ID, OrderID: r.OrderID, Discount: r.Discount, Status: r.Status,
			CreatedAt: r.CreatedAt,
		}
		if r.ReleasedAt.Valid {
			t := r.ReleasedAt.Time
			row.ReleasedAt = &t
		}
		out = append(out, row)
	}
	if len(rows) > limit {
		next := out[len(out)-1].ID
		return out, &next, nil
	}
	return out, nil, nil
}

func pageLimit(n int) int {
	if n < 1 || n > maxLimit {
		return defaultLimit
	}
	return n
}

// CustomerRoot trả hồ sơ gốc (sau gộp) của khách id qua view customer.customers_v; không có → ErrInvalidInput (khách
// do người gọi gửi lên, Q-T71-7).
func CustomerRoot(ctx context.Context, tx pgx.Tx, id uuid.UUID) (uuid.UUID, error) {
	root, err := store.New(tx).CustomerRoot(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("khách gắn mã: %w", ErrInvalidInput)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc hồ sơ gốc của khách: %w", err)
	}
	return root, nil
}

// UsedOrders trả, cho mỗi mã trong couponIDs có lượt dùng còn hiệu lực, đơn của lượt mới nhất (Task 487: "Đã dùng ·
// <mã đơn>"; mã đơn do lớp HTTP ghép bằng order.Codes). Mã chưa dùng không có trong map.
func UsedOrders(ctx context.Context, tx pgx.Tx, couponIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	out := make(map[uuid.UUID]uuid.UUID, len(couponIDs))
	if len(couponIDs) == 0 {
		return out, nil
	}
	rows, err := store.New(tx).CouponUsedOrders(ctx, couponIDs)
	if err != nil {
		return nil, fmt.Errorf("đọc đơn đã dùng mã: %w", err)
	}
	for _, r := range rows {
		out[r.CouponID] = r.OrderID
	}
	return out, nil
}

// MaskedBoundPhone là SĐT gắn mã dạng trong nước che giữa ("0905 *** 781", như màn hình); rỗng khi mã không gắn SĐT.
func (c Coupon) MaskedBoundPhone() string {
	if c.BoundPhone == "" {
		return ""
	}
	d := string(c.BoundPhone)
	if rest, ok := strings.CutPrefix(d, "+84"); ok {
		d = "0" + rest
	}
	if len(d) < 7 {
		return "***"
	}
	return d[:4] + " *** " + d[len(d)-3:]
}
