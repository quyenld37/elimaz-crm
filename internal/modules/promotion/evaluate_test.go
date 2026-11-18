package promotion_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// at trả mốc giờ Việt Nam (UTC+7) cho các mốc của kế hoạch.
func at(y int, mo time.Month, d, h, mi, sec int) time.Time {
	return time.Date(y, mo, d, h, mi, sec, 0, time.FixedZone("ICT", 7*3600))
}

func ptr[T any](v T) *T { return &v }

// baseCoupon là mã gốc của kế hoạch (Task M2-06 Step 1): percent 1000 (10%), max_discount 50000, hạn 01/12/2026–
// 31/12/2026, max_uses 2.
func baseCoupon() promotion.Coupon {
	return promotion.Coupon{
		ID: uuid.New(), Code: "GIAM10", Name: "Giảm 10%", DiscountKind: promotion.KindPercent, Value: 1000,
		MaxDiscount: ptr(int64(50000)), MaxUses: ptr(int32(2)), ReleaseOnCancel: true,
		ValidFrom: at(2026, 12, 1, 0, 0, 0), ValidTo: at(2026, 12, 31, 0, 0, 0),
		Status: promotion.StatusActive,
	}
}

// baseInput: giờ thử 15/12/2026, kênh manual, người bán S1.
func baseInput(base int64) promotion.EvalInput {
	return promotion.EvalInput{
		Base: base, Channel: "manual", CustomerRootID: uuid.New(), Phone: logging.Phone("+84901111111"),
		SellerID: s1(), Now: at(2026, 12, 15, 10, 0, 0),
	}
}

// Người bán S1 (của đơn) và S2.
func s1() uuid.UUID { return uuid.MustParse("00000000-0000-7000-8000-000000000001") }
func s2() uuid.UUID { return uuid.MustParse("00000000-0000-7000-8000-000000000002") }

// evalCase là một ca của TestEvaluate: sửa mã gốc, sửa đầu vào gốc, kết quả mong đợi.
type evalCase struct {
	name     string
	coupon   func(c *promotion.Coupon)
	input    func(in *promotion.EvalInput)
	discount int64
	reason   promotion.Reason
}

// priceCases: cách tính phần giảm (phần trăm, trần, làm tròn, cố định, min_subtotal).
func priceCases() []evalCase {
	return []evalCase{
		{name: "10% chặn bởi max_discount", input: func(in *promotion.EvalInput) { in.Base = 660000 }, discount: 50000},
		{name: "10% dưới trần", input: func(in *promotion.EvalInput) { in.Base = 400000 }, discount: 40000},
		{name: "làm tròn xuống", input: func(in *promotion.EvalInput) { in.Base = 333333 }, discount: 33333},
		{
			name: "không có trần", coupon: func(c *promotion.Coupon) { c.MaxDiscount = nil },
			input: func(in *promotion.EvalInput) { in.Base = 660000 }, discount: 66000,
		},
		{
			name: "100% không quá Base", coupon: func(c *promotion.Coupon) { c.Value = 10000; c.MaxDiscount = nil },
			input: func(in *promotion.EvalInput) { in.Base = 123457 }, discount: 123457,
		},
		{
			name: "Base lớn nhất không tràn", coupon: func(c *promotion.Coupon) { c.Value = 9999; c.MaxDiscount = nil },
			input: func(in *promotion.EvalInput) { in.Base = 9007199254740991 }, discount: 9006298534815516,
		},
		{name: "cố định lớn hơn Base", coupon: func(c *promotion.Coupon) {
			c.DiscountKind, c.Value, c.MaxDiscount = promotion.KindFixed, 30000, nil
		}, input: func(in *promotion.EvalInput) { in.Base = 20000 }, discount: 20000},
		{name: "cố định nhỏ hơn Base", coupon: func(c *promotion.Coupon) {
			c.DiscountKind, c.Value, c.MaxDiscount = promotion.KindFixed, 30000, nil
		}, input: func(in *promotion.EvalInput) { in.Base = 500000 }, discount: 30000},
		{
			name: "dưới min_subtotal", coupon: func(c *promotion.Coupon) { c.MinSubtotal = 300000 },
			input: func(in *promotion.EvalInput) { in.Base = 299999 }, reason: promotion.ReasonMinSubtotal,
		},
		{
			name: "đúng min_subtotal", coupon: func(c *promotion.Coupon) { c.MinSubtotal = 300000 },
			input: func(in *promotion.EvalInput) { in.Base = 300000 }, discount: 30000,
		},
	}
}

// ruleCases: các lý do từ chối và thứ tự kiểm.
func ruleCases() []evalCase {
	bound := uuid.MustParse("00000000-0000-7000-8000-0000000000b0")
	return []evalCase{
		{name: "chưa tới hạn", input: func(in *promotion.EvalInput) {
			in.Base, in.Now = 400000, at(2026, 11, 30, 23, 59, 59)
		}, reason: promotion.ReasonNotStarted},
		{name: "đúng valid_from", input: func(in *promotion.EvalInput) {
			in.Base, in.Now = 400000, at(2026, 12, 1, 0, 0, 0)
		}, discount: 40000},
		{name: "đúng valid_to là hết hạn", input: func(in *promotion.EvalInput) {
			in.Base, in.Now = 400000, at(2026, 12, 31, 0, 0, 0)
		}, reason: promotion.ReasonExpired},
		{
			name: "kênh không có trong danh sách", coupon: func(c *promotion.Coupon) { c.Channels = []string{"web"} },
			input: func(in *promotion.EvalInput) { in.Base = 400000 }, reason: promotion.ReasonChannel,
		},
		{
			name: "kênh có trong danh sách", coupon: func(c *promotion.Coupon) { c.Channels = []string{"web", "manual"} },
			input: func(in *promotion.EvalInput) { in.Base = 400000 }, discount: 40000,
		},
		{
			name: "gắn SĐT khác", coupon: func(c *promotion.Coupon) { c.BoundPhone = "+84912345678" },
			input: func(in *promotion.EvalInput) { in.Base = 400000 }, reason: promotion.ReasonBoundOther,
		},
		{
			name: "gắn SĐT đúng (viết kiểu 0…)", coupon: func(c *promotion.Coupon) { c.BoundPhone = "+84912345678" },
			input: func(in *promotion.EvalInput) { in.Base, in.Phone = 400000, "0912 345 678" }, discount: 40000,
		},
		{
			name: "gắn SĐT, đơn không có SĐT", coupon: func(c *promotion.Coupon) { c.BoundPhone = "+84912345678" },
			input: func(in *promotion.EvalInput) { in.Base, in.Phone = 400000, "" }, reason: promotion.ReasonBoundOther,
		},
		{
			name: "gắn khách khác", coupon: func(c *promotion.Coupon) { c.BoundCustomerID = bound },
			input: func(in *promotion.EvalInput) { in.Base = 400000 }, reason: promotion.ReasonBoundOther,
		},
		{
			name: "gắn đúng khách", coupon: func(c *promotion.Coupon) { c.BoundCustomerID = bound },
			input: func(in *promotion.EvalInput) { in.Base, in.CustomerRootID = 400000, bound }, discount: 40000,
		},
		{name: "gắn khách và SĐT, khớp SĐT", coupon: func(c *promotion.Coupon) {
			c.BoundCustomerID, c.BoundPhone = bound, "+84912345678"
		}, input: func(in *promotion.EvalInput) { in.Base, in.Phone = 400000, "+84912345678" }, discount: 40000},
		{
			name: "đã tắt", coupon: func(c *promotion.Coupon) { c.Status = promotion.StatusDisabled },
			input: func(in *promotion.EvalInput) { in.Base = 400000 }, reason: promotion.ReasonDisabled,
		},
		{
			name: "mã của người bán khác", coupon: func(c *promotion.Coupon) { c.SellerID = s2() },
			input: func(in *promotion.EvalInput) { in.Base = 400000 }, reason: promotion.ReasonSeller,
		},
		{
			name: "mã của đúng người bán", coupon: func(c *promotion.Coupon) { c.SellerID = s1() },
			input: func(in *promotion.EvalInput) { in.Base = 400000 }, discount: 40000,
		},
		// Thứ tự kiểm: disabled → not_started → expired → channel → seller → bound_other → min_subtotal.
		{
			name: "tắt đứng trước chưa tới hạn", coupon: func(c *promotion.Coupon) { c.Status = promotion.StatusDisabled },
			input:  func(in *promotion.EvalInput) { in.Now = at(2026, 11, 1, 0, 0, 0) },
			reason: promotion.ReasonDisabled,
		},
		{name: "kênh đứng trước người bán", coupon: func(c *promotion.Coupon) {
			c.Channels, c.SellerID = []string{"web"}, s2()
		}, input: func(in *promotion.EvalInput) { in.Base = 400000 }, reason: promotion.ReasonChannel},
		{name: "người bán đứng trước gắn khách", coupon: func(c *promotion.Coupon) {
			c.SellerID, c.BoundCustomerID = s2(), bound
		}, input: func(in *promotion.EvalInput) { in.Base = 400000 }, reason: promotion.ReasonSeller},
		{name: "gắn khách đứng trước min_subtotal", coupon: func(c *promotion.Coupon) {
			c.BoundCustomerID, c.MinSubtotal = bound, 300000
		}, input: func(in *promotion.EvalInput) { in.Base = 1 }, reason: promotion.ReasonBoundOther},
		{
			name: "không kiểm lượt", coupon: func(c *promotion.Coupon) { c.UsedCount = 2 },
			input: func(in *promotion.EvalInput) { in.Base = 400000 }, discount: 40000,
		},
	}
}

func TestEvaluate(t *testing.T) {
	t.Parallel()
	for _, tc := range append(priceCases(), ruleCases()...) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := baseCoupon()
			if tc.coupon != nil {
				tc.coupon(&c)
			}
			in := baseInput(0)
			if tc.input != nil {
				tc.input(&in)
			}
			d, r := promotion.Evaluate(c, in)
			if d != tc.discount || r != tc.reason {
				t.Fatalf("Evaluate = (%d, %q), mong (%d, %q)", d, r, tc.discount, tc.reason)
			}
		})
	}
}
