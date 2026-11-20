package cash

// Loại bút toán (05 mục 2). M1 chỉ ghi expected, collected; ba loại sau có kiểu nhưng chưa có đường gọi (M3).
const (
	KindExpected   = "expected"
	KindCollected  = "collected"
	KindRemitted   = "remitted"
	KindReturned   = "returned"
	KindAdjustment = "adjustment"
)

// Ngăn tiền (05 mục 2).
const (
	// AccountToCollect: hãng đang đi giao, sẽ thu của khách.
	AccountToCollect = "to_collect"
	// AccountCarrierHolding: hãng đã thu, đang giữ, chưa chuyển cho shop.
	AccountCarrierHolding = "carrier_holding"
	// AccountShopReceived: tiền đã về shop theo bảng kê.
	AccountShopReceived = "shop_received"
	// AccountCarrierFee: phí hãng trừ lại khi chuyển tiền.
	AccountCarrierFee = "carrier_fee"
	// AccountSalesClearing: vế đối ứng, giá trị COD đã giao cho hãng đi thu.
	AccountSalesClearing = "sales_clearing"
)

// Tác nhân của bút toán.
const (
	ActorStaff   = "staff"
	ActorSystem  = "system"
	ActorCarrier = "carrier"
)
