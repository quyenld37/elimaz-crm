package reporting_test

import (
	"encoding/json"
	"errors"
	"testing"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// Defs có đủ chín khóa như CHECK của report_runs, mỗi khóa đúng họ, quyền đọc; mỗi Def có kiểu tham số; products chỉ
// xuất tệp (không có JSON).
func TestDefs(t *testing.T) {
	t.Parallel()
	want := map[reporting.Key]struct {
		f    reporting.Family
		read identity.Permission
	}{
		"dashboard":       {"sales", "report.sales.read"},
		"orders":          {"sales", "report.sales.read"},
		"order_drilldown": {"sales", "report.sales.read"},
		"carriers":        {"sales", "report.sales.read"},
		"best_sellers":    {"sales", "report.sales.read"},
		"stock":           {"inventory", "report.inventory.read"},
		"stock_movements": {"inventory", "report.inventory.read"},
		"products":        {"inventory", "report.inventory.read"},
		"customers":       {"customers", "customer.export"},
	}
	defs := reporting.Defs()
	if len(defs) != len(want) {
		t.Fatalf("Defs có %d khóa, mong %d", len(defs), len(want))
	}
	for k, w := range want {
		d, ok := defs[k]
		if !ok {
			t.Errorf("thiếu khóa %s", k)
			continue
		}
		if d.Key != k || d.Family != w.f || d.Read != w.read {
			t.Errorf("%s: key=%s family=%s read=%s", k, d.Key, d.Family, d.Read)
		}
		if d.NewParams == nil || d.NewParams() == nil {
			t.Errorf("%s: không có kiểu tham số", k)
		}
	}
	if defs[reporting.KeyProducts].JSON != nil {
		t.Error("products có JSON, mong chỉ xuất tệp")
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()
	if d, err := reporting.Lookup(reporting.FamilySales, reporting.KeyOrders); err != nil || d.Key != reporting.KeyOrders {
		t.Fatalf("Lookup(sales, orders) = %+v, %v", d, err)
	}
	for _, c := range []struct {
		f reporting.Family
		k reporting.Key
	}{
		{"inventory", "orders"}, {"sales", "products"}, {"customers", "dashboard"}, {"sales", "khong-co"}, {"la", "orders"},
	} {
		if _, err := reporting.Lookup(c.f, c.k); !errors.Is(err, reporting.ErrUnknownReport) {
			t.Errorf("Lookup(%s, %s) = %v, mong ErrUnknownReport", c.f, c.k, err)
		}
	}
}

// DecodeParams từ chối trường lạ, JSON hỏng, phần thừa sau đối tượng; kiểm ngày như GET; khóa lạ → ErrUnknownReport.
func TestDecodeParams(t *testing.T) {
	t.Parallel()
	p, err := reporting.DecodeParams(reporting.KeyOrders, json.RawMessage(`{"from":"2026-11-01","to":"2026-11-02"}`))
	if err != nil {
		t.Fatal(err)
	}
	op, ok := p.(*reporting.OrdersParams)
	if !ok || op.From != "2026-11-01" || op.To != "2026-11-02" {
		t.Fatalf("OrdersParams = %#v", p)
	}
	for _, c := range []struct {
		name string
		k    reporting.Key
		raw  string
		want error
	}{
		{"trường lạ", "orders", `{"from":"2026-11-01","to":"2026-11-02","x":1}`, reporting.ErrBadParams},
		{"JSON hỏng", "orders", `{"from":`, reporting.ErrBadParams},
		{"không phải đối tượng", "orders", `[1]`, reporting.ErrBadParams},
		{"phần thừa", "orders", `{"from":"2026-11-01","to":"2026-11-02"} {}`, reporting.ErrBadParams},
		{"rỗng", "orders", ``, reporting.ErrBadParams},
		{"ngày sai", "dashboard", `{"from":"2026-02-30","to":"2026-03-01"}`, reporting.ErrBadDate},
		{"thiếu ngày", "carriers", `{}`, reporting.ErrBadDate},
		{"from sau to", "best_sellers", `{"from":"2026-11-03","to":"2026-11-02"}`, reporting.ErrFromAfterTo},
		{"367 ngày", "stock_movements", `{"from":"2026-01-01","to":"2027-01-02"}`, reporting.ErrRangeTooLong},
		{"khóa lạ", "khong-co", `{}`, reporting.ErrUnknownReport},
		{"trường lạ của tệp sản phẩm", "products", `{"ten":"x"}`, reporting.ErrBadParams},
	} {
		if _, err := reporting.DecodeParams(c.k, json.RawMessage(c.raw)); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, mong %v", c.name, err, c.want)
		}
	}
	for _, k := range []reporting.Key{"products", "customers", "stock"} {
		if _, err := reporting.DecodeParams(k, json.RawMessage(`{}`)); err != nil {
			t.Errorf("%s {}: %v", k, err)
		}
	}
}
