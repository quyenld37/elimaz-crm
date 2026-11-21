package vtp

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Hằng của tài liệu: PRODUCT_TYPE "HH" là hàng; TYPE / NATIONAL_TYPE 1 là bảng giá trong nước.
const (
	productGoods = "HH"
	domestic     = 1
)

// Cấp địa giới của CRM (fulfillment.LocationMap).
const (
	levelProvince = "province"
	levelWard     = "ward"
)

// route là ID tỉnh, phường của VTP (danh mục V3) cho nơi gửi và nơi nhận.
type route struct {
	fromProvince, fromWard, toProvince, toWard int64
}

// mapRoute đổi mã CRM của nơi gửi và nơi nhận sang ID VTP qua acc.Locations; thiếu mã hay mã hãng không phải số →
// address_unmapped (bọc ErrAddressUnmapped), không gọi mạng. Mã chưa khớp không bao giờ được gửi đoán (dia-gioi.md).
func mapRoute(acc fulfillment.Account, from, to fulfillment.Address) (route, error) {
	var r route
	ok := acc.Locations != nil &&
		carrierID(acc.Locations, levelProvince, from.ProvinceCode, &r.fromProvince) &&
		carrierID(acc.Locations, levelWard, from.WardCode, &r.fromWard) &&
		carrierID(acc.Locations, levelProvince, to.ProvinceCode, &r.toProvince) &&
		carrierID(acc.Locations, levelWard, to.WardCode, &r.toWard)
	if !ok {
		return route{}, &fulfillment.CarrierError{
			Class: fulfillment.ClassRejected, Code: codeUnmapped,
			Err: fmt.Errorf("%w: %w", fulfillment.ErrAddressUnmapped, fulfillment.ErrRejected),
		}
	}
	return r, nil
}

func carrierID(m fulfillment.LocationMap, level, our string, out *int64) bool {
	if our == "" {
		return false
	}
	c, ok := m.Carrier(level, our)
	if !ok {
		return false
	}
	n, err := strconv.ParseInt(c, 10, 64)
	if err != nil || n <= 0 {
		return false
	}
	*out = n
	return true
}

// checkOptions: tùy chọn khai false trong Capabilities (cho xem/thử hàng, phí khi khách không nhận) → ErrNotSupported.
func checkOptions(o fulfillment.Options) error {
	if (o.Inspection != "" && o.Inspection != "none") || o.FeeOnRefuse > 0 {
		return fmt.Errorf("tùy chọn giao của Viettel Post: %w", fulfillment.ErrNotSupported)
	}
	return nil
}

// priceBody là thân chung của getPriceAll và getPrice (trang "Tính cước", "Lấy danh sách dịch vụ" địa chỉ ID).
// *_DISTRICT để null: địa chỉ 2 cấp, như mẫu createOrder của tài liệu (cần xác minh, docs/carriers/vtp.md).
type priceBody struct {
	SenderProvince   int64   `json:"SENDER_PROVINCE"`
	SenderDistrict   *int64  `json:"SENDER_DISTRICT"`
	SenderWard       int64   `json:"SENDER_WARD"`
	ReceiverProvince int64   `json:"RECEIVER_PROVINCE"`
	ReceiverDistrict *int64  `json:"RECEIVER_DISTRICT"`
	ReceiverWard     int64   `json:"RECEIVER_WARD"`
	ProductType      string  `json:"PRODUCT_TYPE"`
	ProductWeight    int     `json:"PRODUCT_WEIGHT"`
	ProductPrice     int64   `json:"PRODUCT_PRICE"`
	MoneyCollection  int64   `json:"MONEY_COLLECTION"`
	ProductLength    int     `json:"PRODUCT_LENGTH"`
	ProductWidth     int     `json:"PRODUCT_WIDTH"`
	ProductHeight    int     `json:"PRODUCT_HEIGHT"`
	Type             int     `json:"TYPE,omitempty"`
	NationalType     int     `json:"NATIONAL_TYPE,omitempty"`
	OrderService     string  `json:"ORDER_SERVICE,omitempty"`
	OrderServiceAdd  *string `json:"ORDER_SERVICE_ADD,omitempty"`
}

func newPriceBody(r route, p fulfillment.Parcel, cod int64) priceBody {
	return priceBody{
		SenderProvince: r.fromProvince, SenderWard: r.fromWard, ReceiverProvince: r.toProvince, ReceiverWard: r.toWard,
		ProductType: productGoods, ProductWeight: p.WeightGrams, ProductPrice: p.DeclaredValue, MoneyCollection: cod,
		ProductLength: p.LengthCm, ProductWidth: p.WidthCm, ProductHeight: p.HeightCm,
	}
}

// serviceQuote là một phần tử trả về của getPriceAll.
type serviceQuote struct {
	Code  string `json:"MA_DV_CHINH"`
	Name  string `json:"TEN_DICHVU"`
	Price int64  `json:"GIA_CUOC"`
	Time  string `json:"THOI_GIAN"`
}

// priceData là data của getPrice.
type priceData struct {
	Total         int64   `json:"MONEY_TOTAL"`
	Main          int64   `json:"MONEY_TOTAL_FEE"`
	Fuel          int64   `json:"MONEY_FEE"`
	COD           int64   `json:"MONEY_COLLECTION_FEE"`
	Other         int64   `json:"MONEY_OTHER_FEE"`
	VAS           int64   `json:"MONEY_VAS"`
	VAT           int64   `json:"MONEY_VAT"`
	DeliveryHours float64 `json:"KPI_HT"`
}

// Quote báo phí: không chọn dịch vụ → getPriceAll (mọi dịch vụ, Total = GIA_CUOC); có dịch vụ → getPrice (Total =
// MONEY_TOTAL, các phần cước vào Breakdown). Chỉ gửi ID địa giới, cân nặng, kích thước, tiền; không gửi tên, SĐT.
func (a *Adapter) Quote(ctx context.Context, acc fulfillment.Account, in fulfillment.QuoteInput) ([]fulfillment.QuoteResult, error) {
	if err := checkOptions(in.Options); err != nil {
		return nil, err
	}
	r, err := mapRoute(acc, in.From, in.To)
	if err != nil {
		return nil, fmt.Errorf("báo phí Viettel Post: %w", err)
	}
	if in.Service == "" {
		ss, err := a.priceAll(ctx, acc, r, in.Parcel, in.COD)
		if err != nil {
			return nil, err
		}
		out := make([]fulfillment.QuoteResult, 0, len(ss))
		for _, s := range ss {
			out = append(out, fulfillment.QuoteResult{
				Service: s.Code, ServiceName: s.Name, Total: s.Price, ExpectedDays: days(s.Time),
			})
		}
		return out, nil
	}
	body := newPriceBody(r, in.Parcel, in.COD)
	body.NationalType, body.OrderService, body.OrderServiceAdd = domestic, in.Service, new("")
	var d priceData
	if err := a.call(ctx, acc, kindQuote, http.MethodPost, "/v2/order/getPrice", body, &d); err != nil {
		return nil, fmt.Errorf("báo phí Viettel Post: %w", err)
	}
	return []fulfillment.QuoteResult{{
		Service: in.Service, Total: d.Total, ExpectedDays: hoursToDays(d.DeliveryHours),
		Breakdown: map[string]int64{"main": d.Main, "fuel": d.Fuel, "cod": d.COD, "other": d.Other, "vas": d.VAS, "vat": d.VAT},
	}}, nil
}

// priceAll gọi getPriceAll, trả các dịch vụ có mã và giá dương.
func (a *Adapter) priceAll(ctx context.Context, acc fulfillment.Account, r route, p fulfillment.Parcel, cod int64) ([]serviceQuote, error) {
	body := newPriceBody(r, p, cod)
	body.Type = domestic
	var ss []serviceQuote
	if err := a.call(ctx, acc, kindQuote, http.MethodPost, "/v2/order/getPriceAll", body, &ss); err != nil {
		return nil, fmt.Errorf("báo phí Viettel Post: %w", err)
	}
	return slices.DeleteFunc(ss, func(s serviceQuote) bool { return s.Code == "" || s.Price <= 0 }), nil
}

// cheapest chọn dịch vụ có GIA_CUOC thấp nhất (trùng giá: mã nhỏ hơn); rỗng → "".
func cheapest(ss []serviceQuote) string {
	if len(ss) == 0 {
		return ""
	}
	return slices.MinFunc(ss, func(x, y serviceQuote) int {
		return cmp.Or(cmp.Compare(x.Price, y.Price), cmp.Compare(x.Code, y.Code))
	}).Code
}

// days đọc THOI_GIAN dạng "<n> giờ" (làm tròn lên theo ngày) hay "<n> ngày"; dạng khác → 0.
func days(s string) int {
	f := strings.Fields(s)
	if len(f) != 2 {
		return 0
	}
	n, err := strconv.Atoi(f[0])
	if err != nil || n <= 0 {
		return 0
	}
	switch strings.ToLower(f[1]) {
	case "giờ":
		return (n + 23) / 24
	case "ngày":
		return n
	}
	return 0
}

// hoursToDays đổi KPI_HT (giờ) sang số ngày, làm tròn lên.
func hoursToDays(h float64) int {
	if h <= 0 {
		return 0
	}
	return int(math.Ceil(h / 24))
}
