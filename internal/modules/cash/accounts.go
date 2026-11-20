package cash

// Loại bút toán (05 mục 2). M1 chỉ ghi expected, collected; ba loại sau có kiểu nhưng chưa có đường gọi (M3).
const (
	KindExpected   = "expected"
	KindCollected  = "collected"
	KindRemitted   = "remitted"
	KindReturned   = "returned"
	KindAdjustment = "adjustment"
	// KindFee: phí hãng trừ lại (M3).
	KindFee = "fee"
	// KindReversal: đảo một bút toán sai (M3; ghi bằng Reverse).
	KindReversal = "reversal"
)

// isKind báo kind là loại bút toán đã biết (khớp CHECK cod_transfers_kind_check của 0303).
func isKind(kind string) bool {
	switch kind {
	case KindExpected, KindCollected, KindRemitted, KindReturned, KindAdjustment, KindFee, KindReversal:
		return true
	}
	return false
}

// needsReason báo loại kind bắt buộc có lý do; loại khác bắt buộc lý do rỗng (CHECK cod_transfers_reason_kind).
func needsReason(kind string) bool { return kind == KindAdjustment || kind == KindReversal }

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

// isAccount báo a là ngăn tiền đã biết (khớp CHECK account của cash.cod_ledger_entries).
func isAccount(a string) bool {
	switch a {
	case AccountToCollect, AccountCarrierHolding, AccountShopReceived, AccountCarrierFee, AccountSalesClearing:
		return true
	}
	return false
}

// Tác nhân của bút toán.
const (
	ActorStaff   = "staff"
	ActorSystem  = "system"
	ActorCarrier = "carrier"
)
