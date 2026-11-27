package reporting_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/reporting"
)

// EXPLAIN bằng crm_app của xuất nhập tồn kỳ 16/11 trên 1.200 biến thể có sổ: không Seq Scan trên stock_ledger (phạm vi
// all, phạm vi một kho, có tiền tố); kỳ thiếu → ErrBadDate trước khi có câu.
func TestMovementsExplainNoLedgerSeqScan(t *testing.T) {
	t.Parallel()
	e := newStockEnv(t)
	e.bulkVariants(t, 1200)
	own := e.seed.Own()
	for _, c := range []struct {
		name string
		s    reporting.DataScope
		pg   reporting.StockPage
	}{
		{"all", reporting.DataScope{TenantID: e.tenant, SellerID: own, AllWarehouses: true}, reporting.StockPage{}},
		{"một kho", reporting.DataScope{TenantID: e.tenant, SellerID: own, WarehouseIDs: []uuid.UUID{e.wh}}, reporting.StockPage{}},
		{"tiền tố", reporting.DataScope{TenantID: e.tenant, SellerID: own, AllWarehouses: true}, reporting.StockPage{SKUPrefix: "QU-"}},
	} {
		q, args, err := reporting.MovementsQuery(c.s, reporting.MovementsParams{PeriodParams: period("2026-11-16", "2026-11-16").PeriodParams, StockPage: c.pg})
		requireNoSeqScanOn(t, c.name, "stock_ledger", e.explainScans(t, q, args, err))
	}
	if _, _, err := reporting.MovementsQuery(reporting.DataScope{}, reporting.MovementsParams{}); !errors.Is(err, reporting.ErrBadDate) {
		t.Errorf("kỳ thiếu: %v", err)
	}
}
