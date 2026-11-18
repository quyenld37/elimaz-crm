package promotion_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/logging"
)

func (e *env) actor() promotion.Actor { return promotion.Actor{TenantID: e.tenant, UserID: e.user} }

// sale10 là mã KM01: 10%, trần 50.000, 01/12–31/12/2026 (hết hạn lúc 00:00 01/01/2027 giờ Việt Nam), 2 lượt.
func sale10(code string) promotion.CouponInput {
	vn := time.FixedZone("ICT", 7*3600)
	return promotion.CouponInput{
		Code: code, Name: "Giảm 10% tháng 12", DiscountKind: promotion.KindPercent, Value: 1000,
		MaxDiscount: ptr(int64(50000)), MaxUses: ptr(int32(2)), ReleaseOnCancel: true,
		ValidFrom: time.Date(2026, 12, 1, 0, 0, 0, 0, vn), ValidTo: time.Date(2027, 1, 1, 0, 0, 0, 0, vn),
	}
}

func (e *env) create(in promotion.CouponInput) (promotion.Coupon, error) {
	return e.createWith(promotion.Writer{}, in)
}

func (e *env) createWith(w promotion.Writer, in promotion.CouponInput) (promotion.Coupon, error) {
	var c promotion.Coupon
	err := e.app(func(tx pgx.Tx) error {
		var err error
		c, err = w.CreateCoupon(e.t.Context(), tx, e.actor(), in)
		return err //nolint:wrapcheck // test
	})
	return c, err
}

func (e *env) update(id uuid.UUID, version int64, p promotion.CouponPatch) (promotion.CouponChange, error) {
	var ch promotion.CouponChange
	err := e.app(func(tx pgx.Tx) error {
		var err error
		ch, err = promotion.Writer{}.UpdateCoupon(e.t.Context(), tx, e.actor(), id, version, p)
		return err //nolint:wrapcheck // test
	})
	return ch, err
}

func (e *env) get(id uuid.UUID) promotion.Coupon {
	e.t.Helper()
	var c promotion.Coupon
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		c, err = promotion.GetCoupon(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return c
}

func TestCreateCoupon_CodeAndTaken(t *testing.T) {
	e := newEnv(t)
	c, err := e.create(sale10("SALE10"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Code != "SALE10" || c.Version != 1 || c.UsedCount != 0 || c.Status != promotion.StatusActive ||
		c.CreatedBy != e.user || *c.MaxUses != 2 || *c.MaxDiscount != 50000 || !c.ReleaseOnCancel {
		t.Fatalf("mã vừa tạo: %+v", c)
	}
	if rem := c.RemainingUses(); rem == nil || *rem != 2 {
		t.Fatalf("lượt còn: %v", rem)
	}
	if _, err := e.create(sale10("sale10")); !errors.Is(err, promotion.ErrCodeTaken) {
		t.Fatalf("mã trùng không phân biệt hoa thường: %v", err)
	}
	// Mã trùng không làm hỏng transaction: tạo mã khác trong cùng transaction vẫn được.
	if err := e.app(func(tx pgx.Tx) error {
		if _, err := (promotion.Writer{}).CreateCoupon(t.Context(), tx, e.actor(), sale10("Sale10")); !errors.Is(err, promotion.ErrCodeTaken) {
			t.Errorf("lần hai: %v", err)
		}
		_, err := promotion.Writer{}.CreateCoupon(t.Context(), tx, e.actor(), sale10("SALE20"))
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("tạo tiếp sau mã trùng: %v", err)
	}
	gen, err := e.create(sale10(""))
	if err != nil {
		t.Fatal(err)
	}
	if !generatedCode.MatchString(gen.Code) {
		t.Fatalf("mã sinh: %q", gen.Code)
	}
	unlimited := sale10("NOLIMIT")
	unlimited.MaxUses = nil
	u, err := e.create(unlimited)
	if err != nil || u.RemainingUses() != nil {
		t.Fatalf("không giới hạn lượt: %v %v", err, u.RemainingUses())
	}
}

// Sinh mã trùng mã có sẵn: sinh lại, tối đa 5 lần mỗi mã; cả 5 lần trùng → lỗi.
func TestCreateCoupon_GeneratedRetry(t *testing.T) {
	e := newEnv(t)
	if _, err := e.create(sale10("TAKEN234")); err != nil {
		t.Fatal(err)
	}
	calls := 0
	w := promotion.WithCodeGen(promotion.Writer{}, func(string) (string, error) {
		calls++
		if calls < 5 {
			return "TAKEN234", nil
		}
		return "FRESH234", nil
	})
	c, err := e.createWith(w, sale10(""))
	if err != nil || c.Code != "FRESH234" || calls != 5 {
		t.Fatalf("lần 5 mới được: %v %q %d", err, c.Code, calls)
	}
	calls = 0
	always := promotion.WithCodeGen(promotion.Writer{}, func(string) (string, error) { calls++; return "TAKEN234", nil })
	if _, err := e.createWith(always, sale10("")); err == nil || errors.Is(err, promotion.ErrCodeTaken) || calls != 5 {
		t.Fatalf("5 lần trùng: lỗi nội bộ (không phải code_taken), %d lần: %v", calls, err)
	}
}

func TestCreateCoupon_Invalid(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name string
		edit func(*promotion.CouponInput)
		want error
	}{
		{"mã ký tự lạ", func(in *promotion.CouponInput) { in.Code = "SALE 10" }, promotion.ErrInvalidInput},
		{"mã quá ngắn", func(in *promotion.CouponInput) { in.Code = "AB" }, promotion.ErrInvalidInput},
		{"mã quá dài", func(in *promotion.CouponInput) { in.Code = "A234567890123456789012345678901234" }, promotion.ErrInvalidInput},
		{"tên rỗng", func(in *promotion.CouponInput) { in.Name = "  " }, promotion.ErrInvalidInput},
		{"loại lạ", func(in *promotion.CouponInput) { in.DiscountKind = "gift" }, promotion.ErrInvalidInput},
		{"phần trăm quá 100%", func(in *promotion.CouponInput) { in.Value = 10001 }, promotion.ErrInvalidInput},
		{"giá trị 0", func(in *promotion.CouponInput) { in.Value = 0 }, promotion.ErrInvalidInput},
		{"trần 0", func(in *promotion.CouponInput) { in.MaxDiscount = ptr(int64(0)) }, promotion.ErrInvalidInput},
		{"tối thiểu âm", func(in *promotion.CouponInput) { in.MinSubtotal = -1 }, promotion.ErrInvalidInput},
		{"hạn ngược", func(in *promotion.CouponInput) { in.ValidTo = in.ValidFrom }, promotion.ErrInvalidInput},
		{"kênh lạ", func(in *promotion.CouponInput) { in.Channels = []string{"zalo"} }, promotion.ErrInvalidInput},
		{"lượt 0", func(in *promotion.CouponInput) { in.MaxUses = ptr(int32(0)) }, promotion.ErrInvalidInput},
		{"lượt mỗi khách 0", func(in *promotion.CouponInput) { in.PerCustomerLimit = ptr(int32(0)) }, promotion.ErrInvalidInput},
		{"khách không có", func(in *promotion.CouponInput) { in.BoundCustomerID = uuid.New() }, promotion.ErrInvalidInput},
		{"SĐT sai", func(in *promotion.CouponInput) { in.BoundPhone = "12345" }, customer.ErrInvalidPhone},
	}
	for _, c := range cases {
		in := sale10("BAD-" + uuid.NewString()[:6])
		c.edit(&in)
		if _, err := e.create(in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if n := e.count(`SELECT count(*) FROM promotion.coupons`); n != 0 {
		t.Fatalf("đầu vào sai không được ghi: %d mã", n)
	}
}

// Mã gắn khách lưu hồ sơ gốc; mã gắn SĐT lưu dạng +84…; kênh, giới hạn mỗi khách lưu đúng.
func TestCreateCoupon_Bound(t *testing.T) {
	e := newEnv(t)
	root := e.customer()
	child := e.customer()
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE customer.customers SET root_id = $1 WHERE id = $2`, root, child)
		return err //nolint:wrapcheck // test
	})
	in := sale10("BOUND1")
	in.BoundCustomerID, in.BoundPhone = child, "0901 234 567"
	in.Channels, in.PerCustomerLimit = []string{"manual", "facebook"}, ptr(int32(1))
	c, err := e.create(in)
	if err != nil {
		t.Fatal(err)
	}
	if c.BoundCustomerID != root || c.BoundPhone != logging.Phone("+84901234567") || len(c.Channels) != 2 ||
		*c.PerCustomerLimit != 1 {
		t.Fatalf("mã gắn khách: %+v", c)
	}
	if f := c.AuditFields(); f["bound"] != true || f["boundPhone"] != nil {
		t.Fatalf("audit không có SĐT, chỉ có bound: %v", f)
	}
}

func TestUpdateCoupon_BeforeUse(t *testing.T) {
	e := newEnv(t)
	c, err := e.create(sale10("EDIT1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.update(uuid.New(), 1, promotion.CouponPatch{}); !errors.Is(err, promotion.ErrNotFound) {
		t.Fatalf("mã không có: %v", err)
	}
	if _, err := e.update(c.ID, 2, promotion.CouponPatch{Name: ptr("x")}); !errors.Is(err, promotion.ErrVersionConflict) {
		t.Fatalf("version sai: %v", err)
	}
	ch, err := e.update(c.ID, 1, promotion.CouponPatch{
		DiscountKind: ptr(promotion.KindFixed), Value: ptr(int64(30000)), MinSubtotal: ptr(int64(200000)),
		Channels: []string{"web"}, ReleaseOnCancel: ptr(false), PerCustomerLimit: ptr(int32(3)),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := ch.After
	if ch.Before.Value != 1000 || a.DiscountKind != promotion.KindFixed || a.Value != 30000 || a.MinSubtotal != 200000 ||
		len(a.Channels) != 1 || a.ReleaseOnCancel || *a.PerCustomerLimit != 3 || a.Version != 2 || a.Name != c.Name {
		t.Fatalf("sửa trước khi dùng: %+v", a)
	}
	if _, err := e.update(c.ID, 2, promotion.CouponPatch{Value: ptr(int64(0))}); !errors.Is(err, promotion.ErrInvalidInput) {
		t.Fatalf("giá trị 0: %v", err)
	}
	if _, err := e.update(c.ID, 2, promotion.CouponPatch{ValidTo: ptr(a.ValidFrom)}); !errors.Is(err, promotion.ErrInvalidInput) {
		t.Fatalf("hạn ngược: %v", err)
	}
	if _, err := e.update(c.ID, 2, promotion.CouponPatch{Status: ptr("paused")}); !errors.Is(err, promotion.ErrInvalidInput) {
		t.Fatalf("trạng thái lạ: %v", err)
	}
}

// lockedPatches: mỗi phần sửa đổi một trường bị khóa sau khi mã đã có lượt dùng (giá trị khác giá trị hiện tại của
// mã spec{} mặc định: 10%, trần 50000, tối thiểu 0, mọi kênh, release_on_cancel true).
func lockedPatches(validFrom time.Time) map[string]promotion.CouponPatch {
	return map[string]promotion.CouponPatch{
		"discountKind":     {DiscountKind: ptr(promotion.KindFixed)},
		"value":            {Value: ptr(int64(2000))},
		"maxDiscount":      {MaxDiscount: ptr(int64(40000))},
		"minSubtotal":      {MinSubtotal: ptr(int64(1))},
		"channels":         {Channels: []string{"web"}},
		"perCustomerLimit": {PerCustomerLimit: ptr(int32(1))},
		"releaseOnCancel":  {ReleaseOnCancel: ptr(false)},
		"validFrom":        {ValidFrom: ptr(validFrom.Add(-time.Minute))},
	}
}

func TestUpdateCoupon_LockedAfterUse(t *testing.T) {
	e := newEnv(t)
	id, _, orderID := e.redeemed(spec{maxUses: ptr(int32(3))})
	c := e.get(id)
	for name, p := range lockedPatches(c.ValidFrom) {
		if _, err := e.update(id, c.Version, p); !errors.Is(err, promotion.ErrCouponLockedAfterUse) {
			t.Errorf("đổi %s sau khi dùng: %v", name, err)
		}
	}
	// Gửi lại đúng giá trị đang có (màn hình gửi cả form) không bị chặn (Q-T71-4).
	same, err := e.update(id, c.Version, promotion.CouponPatch{Value: ptr(c.Value), Channels: []string{}})
	if err != nil {
		t.Fatalf("gửi lại giá trị cũ: %v", err)
	}
	c = same.After
	if _, err := e.update(id, c.Version, promotion.CouponPatch{MaxUses: ptr(int32(0))}); !errors.Is(err, promotion.ErrInvalidInput) {
		t.Fatalf("max_uses 0: %v", err)
	}
	// Dùng thêm một lượt (used_count 2) rồi hạ max_uses xuống 1 → ErrMaxUsesBelowUsed.
	cust2 := e.customer()
	if _, err := e.redeem(c.Code, e.order(cust2), e.input(cust2, 400000)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.update(id, c.Version, promotion.CouponPatch{MaxUses: ptr(int32(1))}); !errors.Is(err, promotion.ErrMaxUsesBelowUsed) {
		t.Fatalf("max_uses dưới used_count: %v", err)
	}
	newTo := c.ValidTo.Add(24 * time.Hour)
	ch, err := e.update(id, c.Version, promotion.CouponPatch{
		Name: ptr("Đổi tên"), ValidTo: &newTo, MaxUses: ptr(int32(2)), Status: ptr(promotion.StatusDisabled),
	})
	if err != nil {
		t.Fatalf("trường được phép sau khi dùng: %v", err)
	}
	if a := ch.After; a.Name != "Đổi tên" || !a.ValidTo.Equal(newTo) || *a.MaxUses != 2 || a.Status != promotion.StatusDisabled ||
		a.UsedCount != 2 || a.Version != c.Version+1 {
		t.Fatalf("sau khi sửa: %+v", a)
	}
	// Trả hết lượt (used_count về 0) nhưng còn dòng released: vẫn khóa.
	if _, err := e.release(orderID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE promotion.coupons SET used_count = 0 WHERE id = $1`, id)
		return err //nolint:wrapcheck // test
	})
	c = e.get(id)
	if _, err := e.update(id, c.Version, promotion.CouponPatch{Value: ptr(int64(2000))}); !errors.Is(err, promotion.ErrCouponLockedAfterUse) {
		t.Fatalf("còn dòng released: %v", err)
	}
}

// Quy luật: mã đã có lượt dùng — mọi phần sửa có ít nhất một trường khóa đổi giá trị đều bị từ chối và không đổi gì;
// phần sửa chỉ gồm trường được phép (max_uses ≥ used_count) luôn qua.
func TestUpdateCoupon_LockProperty(t *testing.T) {
	e := newEnv(t)
	id, _, _ := e.redeemed(spec{maxUses: ptr(int32(5))})
	locked := lockedPatches(e.get(id).ValidFrom)
	names := make([]string, 0, len(locked))
	for n := range locked {
		names = append(names, n)
	}
	rapid.Check(t, func(rt *rapid.T) {
		c := e.get(id)
		p := promotion.CouponPatch{}
		if rapid.Bool().Draw(rt, "name") {
			p.Name = ptr(rapid.StringMatching(`[a-z]{1,10}`).Draw(rt, "nameValue"))
		}
		if rapid.Bool().Draw(rt, "maxUses") {
			p.MaxUses = ptr(rapid.Int32Range(1, 10).Draw(rt, "maxUsesValue"))
		}
		if rapid.Bool().Draw(rt, "status") {
			p.Status = ptr(rapid.SampledFrom([]string{promotion.StatusActive, promotion.StatusDisabled}).Draw(rt, "statusValue"))
		}
		lockedField := rapid.Bool().Draw(rt, "locked")
		if lockedField {
			l := locked[rapid.SampledFrom(names).Draw(rt, "field")]
			p.DiscountKind, p.Value, p.MaxDiscount, p.MinSubtotal = l.DiscountKind, l.Value, l.MaxDiscount, l.MinSubtotal
			p.Channels, p.PerCustomerLimit, p.ReleaseOnCancel, p.ValidFrom = l.Channels, l.PerCustomerLimit, l.ReleaseOnCancel, l.ValidFrom
		}
		_, err := e.update(id, c.Version, p)
		switch {
		case lockedField:
			if !errors.Is(err, promotion.ErrCouponLockedAfterUse) {
				rt.Fatalf("trường khóa: %v", err)
			}
			if after := e.get(id); after.Version != c.Version {
				rt.Fatalf("bị từ chối mà vẫn đổi version")
			}
		case err != nil:
			rt.Fatalf("chỉ trường được phép: %v", err)
		}
	})
}

// listCodes trả mã của một trang ListCoupons theo f và con trỏ trang sau.
func (e *env) listCodes(f promotion.CouponFilter) ([]string, *uuid.UUID) {
	e.t.Helper()
	var cs []promotion.Coupon
	var next *uuid.UUID
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		cs, next, err = promotion.ListCoupons(e.t.Context(), tx, f)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Code)
	}
	return out, next
}

func TestListCoupons(t *testing.T) {
	e := newEnv(t)
	var ids []uuid.UUID
	for _, code := range []string{"LIST-A", "LIST-B", "OTHER-C"} {
		c, err := e.create(sale10(code))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	off := promotion.CouponPatch{Status: ptr(promotion.StatusDisabled)}
	if _, err := e.update(ids[1], 1, off); err != nil {
		t.Fatal(err)
	}
	list := e.listCodes
	if got, next := list(promotion.CouponFilter{Limit: 50}); len(got) != 3 || got[0] != "OTHER-C" || next != nil {
		t.Fatalf("mới nhất trước: %v %v", got, next)
	}
	got, next := list(promotion.CouponFilter{Limit: 2})
	if len(got) != 2 || next == nil {
		t.Fatalf("trang 1: %v", got)
	}
	if got2, next2 := list(promotion.CouponFilter{Limit: 2, After: next}); len(got2) != 1 || got2[0] != "LIST-A" || next2 != nil {
		t.Fatalf("trang 2: %v", got2)
	}
	if got, _ := list(promotion.CouponFilter{Limit: 50, Q: "list"}); len(got) != 2 {
		t.Fatalf("q theo mã: %v", got)
	}
	if got, _ := list(promotion.CouponFilter{Limit: 50, Q: "THÁNG 12"}); len(got) != 3 {
		t.Fatalf("q theo tên: %v", got)
	}
	if got, _ := list(promotion.CouponFilter{Limit: 50, Status: promotion.StatusDisabled}); len(got) != 1 || got[0] != "LIST-B" {
		t.Fatalf("lọc trạng thái: %v", got)
	}
	if got, _ := list(promotion.CouponFilter{Limit: 50, Q: "50%_"}); len(got) != 0 {
		t.Fatalf("ký tự đặc biệt không phải mẫu: %v", got)
	}
	// Tenant khác không thấy mã của A.
	other := e.staff.Tenant(t, "B")
	var n int
	e.staff.Exec(t, other, func(tx pgx.Tx) error {
		cs, _, err := promotion.ListCoupons(t.Context(), tx, promotion.CouponFilter{Limit: 50})
		n = len(cs)
		return err //nolint:wrapcheck // test
	})
	if n != 0 {
		t.Fatalf("tenant B thấy %d mã", n)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := promotion.GetCoupon(t.Context(), tx, uuid.New())
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, promotion.ErrNotFound) {
		t.Fatalf("GetCoupon không có: %v", err)
	}
}

func TestListRedemptions(t *testing.T) {
	e := newEnv(t)
	id, _, orderID := e.redeemed(spec{})
	if _, err := e.release(orderID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	cust2 := e.customer()
	order2 := e.order(cust2)
	if _, err := e.redeem(e.code(id), order2, e.input(cust2, 400000)); err != nil {
		t.Fatal(err)
	}
	var rs []promotion.RedemptionRow
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		rs, _, err = promotion.ListRedemptions(t.Context(), tx, id, nil, 50)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].OrderID != order2 || rs[0].Status != "active" || rs[0].Discount != 40000 ||
		rs[1].OrderID != orderID || rs[1].Status != "released" || rs[1].ReleasedAt == nil {
		t.Fatalf("lượt dùng: %+v", rs)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, _, err := promotion.ListRedemptions(t.Context(), tx, uuid.New(), nil, 50)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, promotion.ErrNotFound) {
		t.Fatalf("mã không có: %v", err)
	}
}
