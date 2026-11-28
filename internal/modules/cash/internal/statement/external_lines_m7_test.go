package statement_test

import (
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/cash/internal/statement"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Task 265 (M7-20), kiểm lại TI06 cho đơn cũ còn đang giao sau ngày mở: bảng kê 3 dòng (2 khớp vận đơn của CRM, 1 mã
// vận đơn của đơn cũ không có trong CRM) → dòng lạ external ("đơn ngoài hệ thống"), không bút toán, không vận đơn; sổ
// chỉ đổi đúng phần của 2 dòng khớp; "Lệch đối soát" có dòng lạ; 2 vận đơn khớp được chốt như thường.
func TestExternalLines_M7(t *testing.T) {
	t.Parallel()
	e := newFlowEnv(t)
	v1 := e.shipment(ship{tracking: "FK0101", cod: 300000, status: fulfillment.StatusDelivered})
	v2 := e.shipment(ship{tracking: "FK0102", cod: 200000, status: fulfillment.StatusDelivered})
	sumBefore := e.count("SELECT coalesce(sum(abs(amount)), 0)::int FROM cash.cod_ledger_entries")
	transfersBefore := e.count("SELECT count(*) FROM cash.cod_transfers")
	id := e.statement(uuid.Nil, nil, line{"FK0101", 300000, 20000, 0}, line{"VTP0000OLD1", 450000, 30000, 0},
		line{"FK0102", 200000, 20000, 0})
	before := e.get(id)
	if l := lineOf(t, before, "VTP0000OLD1"); l.MatchStatus != statement.MatchExternal || l.ShipmentID != nil {
		t.Fatalf("dòng đơn cũ trước chốt: %+v", l)
	}
	e.mustConfirm(id)
	d := e.get(id)
	for _, code := range []string{"FK0101", "FK0102"} {
		if l := lineOf(t, d, code); l.MatchStatus != statement.MatchMatched || l.PostedTransferID == nil {
			t.Errorf("dòng %s: %+v, mong matched có bút toán", code, l)
		}
	}
	ext := lineOf(t, d, "VTP0000OLD1")
	if ext.MatchStatus != statement.MatchExternal || ext.PostedTransferID != nil || ext.ShipmentID != nil {
		t.Fatalf("dòng đơn cũ sau chốt: %+v", ext)
	}
	if n := e.count("SELECT count(*) FROM cash.cod_transfers") - transfersBefore; n != 2 {
		t.Errorf("bút toán mới: %d, mong 2 (chỉ hai dòng khớp)", n)
	}
	if n := e.count("SELECT count(*) FROM cash.cod_transfers WHERE statement_line_id = $1", ext.ID); n != 0 {
		t.Errorf("bút toán của dòng đơn cũ: %d", n)
	}
	// Mỗi dòng khớp ghi một bút toán remitted: |shop_received| + |carrier_fee| + |carrier_holding| = 2 × cod.
	if got := e.count("SELECT coalesce(sum(abs(amount)), 0)::int FROM cash.cod_ledger_entries") - sumBefore; got != 2*(300000+200000) {
		t.Errorf("sổ đổi %d, mong %d (chỉ hai dòng khớp)", got, 2*(300000+200000))
	}
	for _, v := range []uuid.UUID{v1, v2} {
		if n := e.settledFor(v); n != 1 {
			t.Errorf("cod.settled của %s: %d, mong 1", v, n)
		}
	}
	found := 0
	for _, x := range e.discrepancies() {
		if x.Kind == cash.DiscrepancyStatementLine && x.MatchStatus == statement.MatchExternal && x.TrackingCode == "VTP0000OLD1" &&
			x.ShipmentID == nil && x.StatementID != nil && *x.StatementID == id {
			found++
		}
	}
	if found != 1 {
		t.Errorf("lệch đối soát có %d dòng đơn cũ, mong 1", found)
	}
}
