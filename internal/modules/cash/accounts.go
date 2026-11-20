package cash

import "elimaz.com.vn/crm/internal/modules/cash/internal/postings"

// Loại bút toán (05 mục 2). Bốn loại đầu cùng giá trị với hằng của luật ghi sổ (postings).
const (
	KindExpected   = postings.KindExpected
	KindCollected  = postings.KindCollected
	KindRemitted   = postings.KindRemitted
	KindReturned   = postings.KindReturned
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

// Ngăn tiền (05 mục 2); cùng giá trị với hằng của luật ghi sổ (postings).
const (
	// AccountToCollect: hãng đang đi giao, sẽ thu của khách.
	AccountToCollect = postings.AccountToCollect
	// AccountCarrierHolding: hãng đã thu, đang giữ, chưa chuyển cho shop.
	AccountCarrierHolding = postings.AccountCarrierHolding
	// AccountShopReceived: tiền đã về shop theo bảng kê.
	AccountShopReceived = postings.AccountShopReceived
	// AccountCarrierFee: phí hãng trừ lại khi chuyển tiền.
	AccountCarrierFee = postings.AccountCarrierFee
	// AccountSalesClearing: vế đối ứng, giá trị COD đã giao cho hãng đi thu.
	AccountSalesClearing = postings.AccountSalesClearing
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
