package cash_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
)

// Số dư "hãng đang giữ" theo vận đơn (cash.cod_open_holdings, Task M3-48: danh sách lệch, quá hạn đọc theo index thay
// vì cộng cả sổ) luôn bằng ngăn carrier_holding của cash.shipment_cod_v, chỉ có dòng khi khác 0: dự kiến (không có
// dòng) → đã thu 280.000 → trả một phần 200.000 → đảo lần trả (về 280.000) → trả đủ (không còn dòng). Vận đơn khác
// không bị chạm. crm_app không sửa, xóa tay được bảng (chỉ trigger của sổ ghi).
func TestOpenHoldings_FollowLedger(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	ship, other := e.shipment(280000), e.shipment(150000)
	sys := cash.Actor{Type: cash.ActorSystem}
	post := func(id uuid.UUID, kind, key string, entries ...cash.Entry) uuid.UUID {
		t.Helper()
		var tid uuid.UUID
		if err := e.app(func(tx pgx.Tx) error {
			var err error
			tid, _, err = cash.PostWithID(t.Context(), tx, cash.Transfer{
				ShipmentID: id, Kind: kind, IdempotencyKey: key, Actor: sys, Entries: entries,
			})
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return tid
	}
	holding := func(id uuid.UUID) (table int64, rows int64, view int64) {
		t.Helper()
		table = e.count(`SELECT coalesce(sum(carrier_holding), 0)::bigint FROM cash.cod_open_holdings WHERE shipment_id = $1`, id)
		rows = e.count(`SELECT count(*) FROM cash.cod_open_holdings WHERE shipment_id = $1`, id)
		view = e.count(`SELECT coalesce(sum(carrier_holding), 0)::bigint FROM cash.shipment_cod_v WHERE shipment_id = $1`, id)
		return table, rows, view
	}
	expect := func(step string, want int64) {
		t.Helper()
		table, rows, view := holding(ship)
		wantRows := int64(1)
		if want == 0 {
			wantRows = 0
		}
		if table != want || view != want || rows != wantRows {
			t.Fatalf("%s: bảng %d (%d dòng), view %d; muốn %d", step, table, rows, view, want)
		}
	}
	post(ship, cash.KindExpected, "exp", cash.Entry{Account: cash.AccountToCollect, Amount: 280000},
		cash.Entry{Account: cash.AccountSalesClearing, Amount: -280000})
	expect("dự kiến", 0)
	post(ship, cash.KindCollected, "col", cash.Entry{Account: cash.AccountCarrierHolding, Amount: 280000},
		cash.Entry{Account: cash.AccountToCollect, Amount: -280000})
	expect("đã thu", 280000)
	part := post(ship, cash.KindRemitted, "rem1", cash.Entry{Account: cash.AccountCarrierHolding, Amount: -200000},
		cash.Entry{Account: cash.AccountShopReceived, Amount: 180000}, cash.Entry{Account: cash.AccountCarrierFee, Amount: 20000})
	expect("trả một phần", 80000)
	if err := e.app(func(tx pgx.Tx) error {
		_, err := cash.Reverse(t.Context(), tx, part, "Nhập nhầm bảng kê", sys)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	expect("đảo lần trả", 280000)
	post(ship, cash.KindRemitted, "rem2", cash.Entry{Account: cash.AccountCarrierHolding, Amount: -280000},
		cash.Entry{Account: cash.AccountShopReceived, Amount: 260000}, cash.Entry{Account: cash.AccountCarrierFee, Amount: 20000})
	expect("trả đủ", 0)
	if table, rows, view := holding(other); table != 0 || rows != 0 || view != 0 {
		t.Fatalf("vận đơn khác: bảng %d (%d dòng), view %d", table, rows, view)
	}
	post(other, cash.KindCollected, "col-o", cash.Entry{Account: cash.AccountCarrierHolding, Amount: 150000},
		cash.Entry{Account: cash.AccountToCollect, Amount: -150000})
	for _, q := range []string{
		"UPDATE cash.cod_open_holdings SET carrier_holding = 1",
		"DELETE FROM cash.cod_open_holdings",
		"INSERT INTO cash.cod_open_holdings (tenant_id, shipment_id, carrier_holding) SELECT tenant_id, shipment_id, 1 FROM cash.cod_open_holdings",
	} {
		if err := e.execApp(q); err == nil {
			t.Errorf("crm_app chạy được %q", q)
		}
	}
	if table, rows, _ := holding(other); table != 150000 || rows != 1 {
		t.Fatalf("vận đơn khác sau khi thử sửa tay: %d (%d dòng)", table, rows)
	}
}
