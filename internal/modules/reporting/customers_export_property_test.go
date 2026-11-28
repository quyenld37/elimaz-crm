package reporting_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/xlsx"
	"elimaz.com.vn/crm/internal/testkit"
)

// propCustomer là một hồ sơ sinh ngẫu nhiên; root là chỉ số hồ sơ gốc (bằng chính nó khi chưa gộp).
type propCustomer struct {
	id                          uuid.UUID
	root                        int
	lifecycle                   string
	orders, delivered, returned int
	created                     time.Time
	last                        *time.Time
	tags                        []int
}

// propFilter là bộ lọc sinh ngẫu nhiên: rỗng/-1 = không lọc; ngày là chỉ số ngày từ propBase.
type propFilter struct {
	lifecycle              string
	minOrders              int
	cFrom, cTo, lFrom, lTo int
	tags                   []int
}

// propBase là ngày đầu của dữ liệu sinh ngẫu nhiên (giờ VN).
func propBase() time.Time { return time.Date(2026, 11, 1, 0, 0, 0, 0, reporting.VN) }

func propDay(i int) string { return propBase().AddDate(0, 0, i).Format("2006-01-02") }

// Thuộc tính (rapid; quy luật PQ06): với khách, gộp, nhãn, bộ lọc ngẫu nhiên, tập mã khách của Def.Rows bằng tập tính
// trong Go từ dữ liệu đã dựng (chỉ hồ sơ gốc; nhãn xét cả hồ sơ đã gộp); ô tỷ lệ hoàn của mỗi khách bằng định dạng
// của customer.StatsFor(...).ReturnRateBP (một nguồn, Q-X2), "—" khi chưa có đơn kết thúc.
func TestCustomersExportProperty(t *testing.T) {
	t.Parallel()
	st := testkit.OpenStaff(t)
	n := 0
	rapid.Check(t, func(rt *rapid.T) {
		n++
		tenant := st.Tenant(t, fmt.Sprintf("p%d", n))
		nTags := rapid.IntRange(0, 3).Draw(rt, "tags")
		cs := drawCustomers(rt, nTags)
		codes, tagIDs := insertCustomers(t, st, tenant, cs, nTags)
		f, raw := drawFilter(rt, nTags, tagIDs)
		params, err := reporting.DecodeParams(reporting.KeyCustomers, raw)
		if err != nil {
			rt.Fatalf("params %s: %v", raw, err)
		}
		expect := map[string]bool{}
		for i, c := range cs {
			if c.root == i && f.matches(cs, i) {
				expect[codes[i]] = true
			}
		}
		got, err := exportRates(t, st, tenant, params, cs, codes, expect)
		if err != nil {
			rt.Fatalf("params %s: %v", raw, err)
		}
		if gotCodes, wantCodes := keys(got), keys(expect); !slices.Equal(gotCodes, wantCodes) {
			rt.Fatalf("params %s: mã %v, mong %v", raw, gotCodes, wantCodes)
		}
	})
}

// drawCustomers sinh 1–8 hồ sơ; hồ sơ sau có thể gộp vào gốc của một hồ sơ trước.
func drawCustomers(rt *rapid.T, nTags int) []propCustomer {
	base := propBase()
	hour := func(label string) time.Time {
		return base.Add(time.Duration(rapid.IntRange(0, 10*24-1).Draw(rt, label)) * time.Hour)
	}
	cs := make([]propCustomer, rapid.IntRange(1, 8).Draw(rt, "n"))
	for i := range cs {
		c := propCustomer{
			id: uuid.New(), root: i, lifecycle: rapid.SampledFrom([]string{"lead", "customer"}).Draw(rt, "life"),
			orders: rapid.IntRange(0, 5).Draw(rt, "orders"), delivered: rapid.IntRange(0, 4).Draw(rt, "del"),
			returned: rapid.IntRange(0, 4).Draw(rt, "ret"), created: hour("created"),
		}
		if i > 0 && rapid.Bool().Draw(rt, "merged") {
			c.root = cs[rapid.IntRange(0, i-1).Draw(rt, "into")].root
		}
		if rapid.Bool().Draw(rt, "bought") {
			c.last = new(hour("last"))
		}
		for k := range nTags {
			if rapid.Bool().Draw(rt, "tagged") {
				c.tags = append(c.tags, k)
			}
		}
		cs[i] = c
	}
	return cs
}

// insertCustomers chèn cs (gốc trước con) và nTags nhãn; trả mã KH… theo chỉ số và mã nhãn.
func insertCustomers(t *testing.T, st *testkit.Staff, tenant uuid.UUID, cs []propCustomer, nTags int) ([]string, []uuid.UUID) {
	t.Helper()
	codes := make([]string, len(cs))
	for i, c := range cs {
		codes[i] = addCustomer(t, st, tenant, custSpec{
			id: c.id, root: cs[c.root].id, name: "K", lifecycle: c.lifecycle, orders: c.orders,
			delivered: c.delivered, returned: c.returned, lastOrder: c.last, created: c.created,
		})
	}
	tagIDs := make([]uuid.UUID, nTags)
	for k := range tagIDs {
		var ids []uuid.UUID
		for _, c := range cs {
			if slices.Contains(c.tags, k) {
				ids = append(ids, c.id)
			}
		}
		tagIDs[k] = addTag(t, st, tenant, fmt.Sprintf("T%d", k), ids...)
	}
	return codes, tagIDs
}

// drawFilter sinh bộ lọc và JSON tham số của nó.
func drawFilter(rt *rapid.T, nTags int, tagIDs []uuid.UUID) (propFilter, json.RawMessage) {
	f := propFilter{minOrders: -1}
	p := map[string]any{}
	if rapid.Bool().Draw(rt, "fLife") {
		f.lifecycle = rapid.SampledFrom([]string{"lead", "customer"}).Draw(rt, "life")
		p["lifecycle"] = f.lifecycle
	}
	if rapid.Bool().Draw(rt, "fMin") {
		f.minOrders = rapid.IntRange(0, 5).Draw(rt, "min")
		p["minOrders"] = f.minOrders
	}
	f.cFrom, f.cTo = rangeDraw(rt, "created", p, "createdFrom", "createdTo")
	f.lFrom, f.lTo = rangeDraw(rt, "last", p, "lastOrderFrom", "lastOrderTo")
	var ids []string
	for k := range nTags {
		if rapid.Bool().Draw(rt, "pick") {
			f.tags = append(f.tags, k)
			ids = append(ids, tagIDs[k].String())
		}
	}
	if len(ids) > 0 {
		p["tagIds"] = ids
	}
	raw, err := json.Marshal(p)
	if err != nil {
		rt.Fatal(err)
	}
	return f, raw
}

// matches: hồ sơ gốc cs[i] khớp bộ lọc (nhãn xét mọi hồ sơ cùng gốc).
func (f propFilter) matches(cs []propCustomer, i int) bool {
	c := cs[i]
	ok := (f.lifecycle == "" || c.lifecycle == f.lifecycle) && (f.minOrders < 0 || c.orders >= f.minOrders) &&
		inDays(&c.created, f.cFrom, f.cTo) && inDays(c.last, f.lFrom, f.lTo)
	if !ok || len(f.tags) == 0 {
		return ok
	}
	return slices.ContainsFunc(cs, func(m propCustomer) bool {
		return m.root == i && slices.ContainsFunc(m.tags, func(k int) bool { return slices.Contains(f.tags, k) })
	})
}

// exportRates chạy Def.Rows bằng crm_app trong tenant (mã → ô tỷ lệ hoàn) và đối chiếu ô tỷ lệ của mỗi khách mong đợi
// với customer.StatsFor.
func exportRates(t *testing.T, st *testkit.Staff, tenant uuid.UUID, params reporting.Params, cs []propCustomer,
	codes []string, expect map[string]bool,
) (map[string]string, error) {
	t.Helper()
	def := reporting.Defs()[reporting.KeyCustomers]
	got := map[string]string{}
	err := tenancy.WithTenant(t.Context(), st.App, tenant, func(tx pgx.Tx) error {
		clear(got)
		if err := def.Rows(t.Context(), tx, reporting.DataScope{TenantID: tenant}, params, func(cells []xlsx.Cell) error {
			got[cells[0].S] = cells[6].S
			return nil
		}); err != nil {
			return err //nolint:wrapcheck // test
		}
		for i, c := range cs {
			if c.root != i || !expect[codes[i]] {
				continue
			}
			s, err := customer.StatsFor(t.Context(), tx, c.id)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			if w := statsRate(s); got[codes[i]] != w {
				return fmt.Errorf("khách %s: ô tỷ lệ %q, StatsFor → %q", codes[i], got[codes[i]], w)
			}
		}
		return nil
	}, reporting.TxReport(30*time.Second, 5*time.Second))
	if err != nil {
		return nil, fmt.Errorf("xuất danh sách khách: %w", err)
	}
	return got, nil
}

// statsRate là chữ tỷ lệ hoàn mong đợi từ chỉ số của M2: "—" khi chưa có đơn kết thúc.
func statsRate(s customer.Stats) string {
	if s.DeliveredCount+s.ReturnedCount == 0 {
		return "—"
	}
	return fmt.Sprintf("%d,%02d%%", s.ReturnRateBP/100, s.ReturnRateBP%100)
}

// rangeDraw có thể đặt khoảng ngày [from, to] (chỉ số ngày từ propBase, -1 = không đặt) vào p.
func rangeDraw(rt *rapid.T, label string, p map[string]any, fromKey, toKey string) (from, to int) {
	from, to = -1, -1
	if rapid.Bool().Draw(rt, label+"From") {
		from = rapid.IntRange(0, 9).Draw(rt, label+"FromDay")
		p[fromKey] = propDay(from)
	}
	if rapid.Bool().Draw(rt, label+"To") {
		to = rapid.IntRange(max(from, 0), 9).Draw(rt, label+"ToDay")
		p[toKey] = propDay(to)
	}
	return from, to
}

// inDays: at nằm trong [ngày from, hết ngày to] giờ VN (-1 = không giới hạn đầu đó); at nil chỉ đúng khi không giới hạn.
func inDays(at *time.Time, from, to int) bool {
	switch {
	case from < 0 && to < 0:
		return true
	case at == nil:
		return false
	case from >= 0 && at.Before(propBase().AddDate(0, 0, from)):
		return false
	}
	return to < 0 || at.Before(propBase().AddDate(0, 0, to+1))
}
