package vtp

import (
	"cmp"
	"context"
	"fmt"
	"net/http"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// ORDER_PAYMENT của tài liệu: 1 không thu hộ; 2 thu hộ tiền hàng và tiền cước; 3 thu hộ tiền hàng, không thu tiền
// cước; 4 thu hộ tiền cước, không thu tiền hàng.
const (
	payNoCollect     = 1
	payGoodsAndFee   = 2
	payGoodsOnly     = 3
	payFeeOnly       = 4
	payerReceiver    = "receiver"
	errNoServiceCode = "vtp_no_service"
)

// orderPayment chọn ORDER_PAYMENT theo người trả cước (shop | receiver; rỗng = shop) và COD.
func orderPayment(payer string, cod int64) int {
	switch {
	case payer == payerReceiver && cod > 0:
		return payGoodsAndFee
	case payer == payerReceiver:
		return payFeeOnly
	case cod > 0:
		return payGoodsOnly
	}
	return payNoCollect
}

// createBody là thân createOrder (trang "Tạo đơn sử dụng địa chỉ ID"); *_DISTRICT null cho địa chỉ 2 cấp như mẫu.
type createBody struct {
	OrderNumber      string `json:"ORDER_NUMBER"`
	CheckUnique      bool   `json:"CHECK_UNIQUE"`
	GroupAddressID   int64  `json:"GROUPADDRESS_ID"`
	CusID            int64  `json:"CUS_ID"`
	SenderFullname   string `json:"SENDER_FULLNAME"`
	SenderPhone      string `json:"SENDER_PHONE"`
	SenderAddress    string `json:"SENDER_ADDRESS"`
	SenderProvince   int64  `json:"SENDER_PROVINCE"`
	SenderDistrict   *int64 `json:"SENDER_DISTRICT"`
	SenderWard       int64  `json:"SENDER_WARD"`
	ReceiverFullname string `json:"RECEIVER_FULLNAME"`
	ReceiverPhone    string `json:"RECEIVER_PHONE"`
	ReceiverAddress  string `json:"RECEIVER_ADDRESS"`
	ReceiverProvince int64  `json:"RECEIVER_PROVINCE"`
	ReceiverDistrict *int64 `json:"RECEIVER_DISTRICT"`
	ReceiverWard     int64  `json:"RECEIVER_WARD"`
	ProductType      string `json:"PRODUCT_TYPE"`
	ProductQuantity  int    `json:"PRODUCT_QUANTITY,omitempty"`
	ProductPrice     int64  `json:"PRODUCT_PRICE"`
	ProductWeight    int    `json:"PRODUCT_WEIGHT"`
	ProductLength    int    `json:"PRODUCT_LENGTH"`
	ProductWidth     int    `json:"PRODUCT_WIDTH"`
	ProductHeight    int    `json:"PRODUCT_HEIGHT"`
	OrderPayment     int    `json:"ORDER_PAYMENT"`
	OrderService     string `json:"ORDER_SERVICE"`
	OrderServiceAdd  string `json:"ORDER_SERVICE_ADD"`
	OrderNote        string `json:"ORDER_NOTE,omitempty"`
	MoneyCollection  int64  `json:"MONEY_COLLECTION"`
}

// createData là data trả về của createOrder: ORDER_NUMBER là mã vận đơn VTP tự sinh, MONEY_TOTAL là tổng phí.
type createData struct {
	OrderNumber string `json:"ORDER_NUMBER"`
	MoneyTotal  int64  `json:"MONEY_TOTAL"`
}

// CreateShipment gọi createOrder với ORDER_NUMBER = r.Code (mã vận đơn CRM, webhook trả lại ở ORDER_REFERENCE) và
// CHECK_UNIQUE = true: gửi lại cùng mã không tạo đơn thứ hai (hãng trả 206 → lỗi không rõ, chờ người). Dịch vụ rỗng →
// dịch vụ rẻ nhất của getPriceAll (Quyết định Q4 của Task 125). Lỗi theo bảng mã ở docs/carriers/vtp.md: chắc chưa gửi
// (bọc ErrNotSent), hãng từ chối (bọc ErrRejected), còn lại không rõ.
func (a *Adapter) CreateShipment(ctx context.Context, acc fulfillment.Account, r fulfillment.CreateRequest) (fulfillment.CreateResult, error) {
	if err := checkOptions(r.Options); err != nil {
		return fulfillment.CreateResult{}, err
	}
	rt, err := mapRoute(acc, r.From, r.To)
	if err != nil {
		return fulfillment.CreateResult{}, fmt.Errorf("tạo vận đơn Viettel Post: %w", err)
	}
	service := r.Service
	if service == "" {
		ss, err := a.priceAll(ctx, acc, rt, r.Parcel, r.CODAmount)
		if err != nil {
			return fulfillment.CreateResult{}, fmt.Errorf("chọn dịch vụ Viettel Post: %w", asNotSent(err))
		}
		if service = cheapest(ss); service == "" {
			return fulfillment.CreateResult{}, fmt.Errorf("chọn dịch vụ Viettel Post: %w", rejected(errNoServiceCode))
		}
	}
	to := r.To
	body := createBody{
		OrderNumber: r.Code, CheckUnique: true,
		SenderFullname: acc.Pickup.Name, SenderPhone: string(acc.Pickup.Phone), SenderAddress: acc.Pickup.Line,
		SenderProvince: rt.fromProvince, SenderWard: rt.fromWard,
		ReceiverFullname: cmp.Or(to.Name, r.ReceiverName), ReceiverPhone: string(cmp.Or(to.Phone, r.ReceiverPhone)),
		ReceiverAddress: cmp.Or(to.Line, r.Address), ReceiverProvince: rt.toProvince, ReceiverWard: rt.toWard,
		ProductType: productGoods, ProductQuantity: r.Parcel.ItemCount, ProductPrice: r.Parcel.DeclaredValue,
		ProductWeight: r.Parcel.WeightGrams, ProductLength: r.Parcel.LengthCm, ProductWidth: r.Parcel.WidthCm,
		ProductHeight: r.Parcel.HeightCm, OrderPayment: orderPayment(r.Options.Payer, r.CODAmount),
		OrderService: service, OrderNote: r.Options.Note, MoneyCollection: r.CODAmount,
	}
	var d createData
	if err := a.call(ctx, acc, kindCreate, http.MethodPost, "/v2/order/createOrder", body, &d); err != nil {
		return fulfillment.CreateResult{}, fmt.Errorf("tạo vận đơn Viettel Post: %w", err)
	}
	if d.OrderNumber == "" {
		return fulfillment.CreateResult{}, fmt.Errorf("tạo vận đơn Viettel Post: %w", afterSend(kindCreate, codeBadReply))
	}
	return fulfillment.CreateResult{TrackingCode: d.OrderNumber, Fee: d.MoneyTotal, FeeKnown: true}, nil
}
