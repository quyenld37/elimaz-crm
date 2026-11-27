package reporting

import (
	"errors"
	"testing"

	"elimaz.com.vn/crm/internal/testkit"
)

// EXPLAIN bằng crm_app của câu báo cáo hãng kỳ 31 ngày (phạm vi all và phạm vi một Page) trên 100.000 đơn: không Seq
// Scan; tập thiếu người bán, thiếu kỳ → ErrBadParams trước khi có câu nào.
func TestCarriersExplainNoSeqScan(t *testing.T) {
	t.Parallel()
	e := newBulkEnv(t)
	period := mustPeriod(t, "2026-03-01", "2026-03-31")
	for _, c := range []struct {
		name string
		s    DataScope
	}{
		{"all", e.all},
		{"Page", DataScope{TenantID: e.tenant, SellerID: e.all.SellerID, PageIDs: e.seed.Pages()[:1]}},
	} {
		b := &sqlBuilder{}
		set, _, err := carrierSet(c.s, period, b)
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := testkit.ExplainAsApp(t.Context(), e.st.App, e.tenant,
			"SELECT sh.carrier_key, sh.carrier_account_id, "+carrierCounts+" FROM "+set+" GROUP BY sh.carrier_key, sh.carrier_account_id", b.args...)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		testkit.RequireNoSeqScan(t, nodes)
	}
	if _, _, err := carrierSet(DataScope{AllPages: true}, period, &sqlBuilder{}); !errors.Is(err, ErrBadParams) {
		t.Error("thiếu người bán mà không lỗi")
	}
	if _, _, err := carrierSet(e.all, Period{}, &sqlBuilder{}); !errors.Is(err, ErrBadParams) {
		t.Error("thiếu kỳ mà không lỗi")
	}
}
