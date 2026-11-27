package reporting_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// planScan là một nút quét bảng của kế hoạch: loại nút, bảng, chỉ mục.
type planScan struct{ node, relation, index string }

// explainScans chạy EXPLAIN (FORMAT JSON) của q bằng crm_app trong ngữ cảnh tenant (RLS như lúc chạy) và trả mọi nút
// có bảng (testkit.ExplainAsApp chỉ trả loại nút, không trả tên bảng).
func (e stockEnv) explainScans(t *testing.T, q string, args []any, buildErr error) []planScan {
	t.Helper()
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	var raw []byte
	if err := tenancy.WithTenant(t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "EXPLAIN (FORMAT JSON) "+q, args...).Scan(&raw)
	}); err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	type node struct {
		Type     string `json:"Node Type"`
		Relation string `json:"Relation Name"`
		Index    string `json:"Index Name"`
		Plans    []node `json:"Plans"`
	}
	var top []struct {
		Plan node `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	var out []planScan
	var walk func(n node)
	walk = func(n node) {
		if n.Relation != "" {
			out = append(out, planScan{n.Type, n.Relation, n.Index})
		}
		for _, c := range n.Plans {
			walk(c)
		}
	}
	for _, p := range top {
		walk(p.Plan)
	}
	return out
}

// requireNoSeqScanOn: kế hoạch không quét tuần tự bảng rel.
func requireNoSeqScanOn(t *testing.T, name, rel string, scans []planScan) {
	t.Helper()
	for _, s := range scans {
		if s.relation == rel && s.node == "Seq Scan" {
			t.Errorf("%s: Seq Scan trên %s: %v", name, rel, scans)
		}
	}
}

// EXPLAIN bằng crm_app trên 1.200 biến thể đã chụp ngày 15/11: hôm nay, tiền tố SKU dùng chỉ mục variants_sku_prefix;
// ngày đã qua đi từ chỉ mục của bản chụp (ngày, người bán, biến thể); cả trang sau; không Seq Scan trên variants,
// stock_daily_snapshots. Thiếu người bán → ErrBadParams trước khi có câu.
func TestStockExplainSKUPrefix(t *testing.T) {
	t.Parallel()
	e := newStockEnv(t)
	e.bulkVariants(t, 1200)
	e.run0(t)
	e.ownerExec(t, "ANALYZE reporting.stock_daily_snapshots")
	s := reporting.DataScope{TenantID: e.tenant, SellerID: e.seed.Own(), AllWarehouses: true}
	cur := reporting.StockCursor{SKU: "AO-0010", VariantID: uuid.Nil, WarehouseID: uuid.Nil}.String()
	for _, day := range []string{"", "2026-11-15"} {
		for _, pg := range []reporting.StockPage{{SKUPrefix: "AO-001"}, {SKUPrefix: "AO-001", Cursor: cur}} {
			q, args, err := reporting.StockQuery(s, pg, day)
			scans := e.explainScans(t, q, args, err)
			requireNoSeqScanOn(t, "tồn ngày "+day, "variants", scans)
			requireNoSeqScanOn(t, "tồn ngày "+day, "stock_daily_snapshots", scans)
			if day != "" {
				continue
			}
			used := false
			for _, sc := range scans {
				used = used || sc.index == "variants_sku_prefix"
			}
			if !used {
				t.Errorf("tồn ngày %q: không dùng variants_sku_prefix: %v", day, scans)
			}
		}
	}
	if _, _, err := reporting.StockQuery(reporting.DataScope{AllWarehouses: true}, reporting.StockPage{}, ""); !errors.Is(err, reporting.ErrBadParams) {
		t.Errorf("thiếu người bán: %v", err)
	}
}
