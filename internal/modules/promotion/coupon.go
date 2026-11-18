package promotion

import (
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/promotion/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Loại giảm và trạng thái của mã (CHECK của promotion.coupons).
const (
	KindPercent = "percent" // Value là phần vạn (1–10000)
	KindFixed   = "fixed"   // Value là số đồng

	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// Coupon là một mã giảm giá (một dòng promotion.coupons). Trường uuid.Nil, con trỏ nil, chuỗi rỗng là "không đặt":
// SellerID Nil = mọi người bán, Channels rỗng = mọi kênh, MaxUses nil = không giới hạn lượt.
type Coupon struct {
	ID               uuid.UUID
	Code             string
	BatchID          uuid.UUID
	Name             string
	DiscountKind     string
	Value            int64
	MaxDiscount      *int64
	MinSubtotal      int64
	Channels         []string
	SellerID         uuid.UUID
	BoundCustomerID  uuid.UUID     // hồ sơ gốc của khách được dùng mã
	BoundPhone       logging.Phone // dạng +84…; tự che khi ghi log
	MaxUses          *int32
	UsedCount        int32
	PerCustomerLimit *int32
	ReleaseOnCancel  bool
	ValidFrom        time.Time
	ValidTo          time.Time
	Status           string
	CreatedBy        uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Version          int64
}

// couponFrom đổi một dòng sqlc sang Coupon (ba câu đọc mã trả cùng cột nên cùng kiểu dòng sau khi đổi).
func couponFrom(r store.CouponByCodeRow) Coupon {
	c := Coupon{
		ID: r.ID, Code: r.Code, Name: r.Name, DiscountKind: r.DiscountKind, Value: r.Value,
		MaxDiscount: r.MaxDiscount, MinSubtotal: r.MinSubtotal, Channels: r.Channels, MaxUses: r.MaxUses,
		UsedCount: r.UsedCount, PerCustomerLimit: r.PerCustomerLimit, ReleaseOnCancel: r.ReleaseOnCancel,
		ValidFrom: r.ValidFrom, ValidTo: r.ValidTo, Status: r.Status, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt, Version: r.Version,
	}
	if r.BatchID != nil {
		c.BatchID = *r.BatchID
	}
	if r.SellerID != nil {
		c.SellerID = *r.SellerID
	}
	if r.BoundCustomerID != nil {
		c.BoundCustomerID = *r.BoundCustomerID
	}
	if r.BoundPhone != nil {
		c.BoundPhone = logging.Phone(*r.BoundPhone)
	}
	return c
}
