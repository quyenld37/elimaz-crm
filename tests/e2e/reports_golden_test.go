package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/testkit"
)

// Kịch bản chín đơn qua API thật (kế hoạch M6, Task 242 = M6-19): dữ liệu đi qua createOrder, confirmOrder,
// cancelOrder, createShipment, cancelShipment, createReturn, receiveReturn của M2, M3 trên hãng giả thật, không chèn
// thẳng bảng. Ba lớp so: (a) số của báo cáo bằng testdata/reports_golden_expected.json (viết tay trước khi chạy);
// (b) câu đếm độc lập, chạy bằng chủ bảng trên bảng gốc, không dùng view hay mã của reporting; (c) số "chờ" của tổng
// quan bằng danh sách đơn của M2, mỗi ô của báo cáo đơn bằng total của danh sách chi tiết.
//
// Khác bảng ở Task 227 (ghi ở mục "Khác kế hoạch" của docs/tasks/task-242.md): hãng giả chỉ có một khóa hãng "fake"
// nên chiều hãng là fake/unknown; hai Page và người chỉ có P1 do chủ bảng dựng (API chỉ gán Page qua hội thoại M4).

const goldenPath = "testdata/reports_golden_expected.json"

// reportsRun là orderRun đủ M3 và M6 cùng dữ liệu của kịch bản.
type reportsRun struct {
	*orderRun
	day    string               // ngày hôm nay giờ Việt Nam (kỳ của mọi báo cáo)
	names  map[string]string    // mã (nhân viên, nguồn, Page) → tên trong golden
	sku    map[string]uuid.UUID // SKU → biến thể
	orders map[string]string    // O1…O9 → mã đơn
	staff  *testkit.Staff       // chủ bảng + crm_app của stack
	store  *files.MemStore
	dl     *httptest.Server
}

func newReportsRun(t *testing.T) *reportsRun {
	t.Helper()
	r := newOrderRunWith(t, stackOptions{m3: true, m6: true})
	c := &reportsRun{
		orderRun: r, day: time.Now().In(reporting.VN).Format(time.DateOnly), names: map[string]string{},
		sku: map[string]uuid.UUID{}, orders: map[string]string{}, staff: &testkit.Staff{App: r.s.app, Owner: r.s.owner},
	}
	store, ok := r.s.m3.files.(*files.MemStore)
	if !ok {
		t.Fatalf("kho tệp của stack là %T, muốn *files.MemStore", r.s.m3.files)
	}
	c.store = store
	c.dl = httptest.NewServer(store)
	t.Cleanup(c.dl.Close)
	store.SetBaseURL(c.dl.URL)
	return c
}

// --- dựng dữ liệu --------------------------------------------------------------------------------------------------

// products tạo ba mẫu của kịch bản (Áo A có hai cỡ S, M giá 150.000; Quần B 250.000; Váy C 200.000) bằng API và nhập
// mỗi biến thể 20 cái vào kho chính bằng một phiếu nhập.
func (c *reportsRun) products() {
	c.t.Helper()
	type v struct{ sku, color, size string }
	for _, p := range []struct {
		code, name string
		price      int64
		vars       []v
	}{
		{"AOA", "Áo A", 150000, []v{{"AO-A-S", "Đỏ", "S"}, {"AO-A-M", "Đỏ", "M"}}},
		{"QUANB", "Quần B", 250000, []v{{"QUAN-B", "Đen", "M"}}},
		{"VAYC", "Váy C", 200000, []v{{"VAY-C", "Xanh", "M"}}},
	} {
		vars := make([]map[string]any, 0, len(p.vars))
		for _, x := range p.vars {
			vars = append(vars, map[string]any{"sku": x.sku, "color": x.color, "size": x.size, "price": p.price})
		}
		got := c.mustCall(http.MethodPost, "/api/v1/catalog/products", map[string]any{
			"code": p.code, "name": p.name, "status": "active", "variants": vars,
		}, http.StatusCreated)
		list, _ := got["variants"].([]any)
		for _, it := range list {
			m, _ := it.(map[string]any)
			c.sku[str(m["sku"])] = uuid.MustParse(str(m["id"]))
		}
	}
	if len(c.sku) != 4 {
		c.t.Fatalf("biến thể kịch bản = %v, muốn 4", c.sku)
	}
	d := c.mustCall(http.MethodPost, "/api/v1/inventory/documents",
		map[string]any{"warehouseId": c.s.shop.Warehouse, "kind": "import"}, http.StatusCreated)
	id := str(d["id"])
	lines := make([]map[string]any, 0, len(c.sku))
	for _, v := range c.sku {
		lines = append(lines, map[string]any{"variantId": v, "quantity": 20, "unitCost": 80000})
	}
	d = c.mustCall(http.MethodPut, "/api/v1/inventory/documents/"+id+"/lines",
		map[string]any{"version": d["version"], "lines": lines}, http.StatusOK)
	d = c.mustCall(http.MethodPost, "/api/v1/inventory/documents/"+id+"/post", map[string]any{"version": d["version"]}, http.StatusOK)
	if d["status"] != "posted" {
		c.t.Fatalf("phiếu nhập = %v, muốn posted", d)
	}
}

// page tạo một Page Facebook của người bán của shop bằng chủ bảng (API chỉ gán Page qua hội thoại của M4).
func (c *reportsRun) page(name string) uuid.UUID {
	c.t.Helper()
	var id uuid.UUID
	c.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(c.t.Context(), `INSERT INTO channel.channel_accounts (tenant_id, seller_id, channel, external_id, name)
			VALUES (current_setting('app.tenant_id')::uuid, $1, 'facebook', $2, $3) RETURNING id`,
			c.s.shop.Seller, "e2e-"+name, name).Scan(&id)
	})
	c.names[id.String()] = name
	return id
}

// staffUser tạo nhân viên (để gán phụ trách đơn) và nhớ tên.
func (c *reportsRun) staffUser(name string) uuid.UUID {
	c.t.Helper()
	id := c.staff.User(c.t, c.s.tenant, strings.ToLower(name)+"@elimaz.test", "active")
	c.names[id.String()] = name
	return id
}

// source tạo nguồn đơn bằng API và nhớ tên.
func (c *reportsRun) source(name string) uuid.UUID {
	c.t.Helper()
	got := c.mustCall(http.MethodPost, "/api/v1/traffic-sources", map[string]any{"name": name}, http.StatusCreated)
	id := uuid.MustParse(str(got["id"]))
	c.names[id.String()] = name
	return id
}

type line struct {
	sku string
	qty int
}

type orderSpec struct {
	name, channel, intent string
	page, source, care    uuid.UUID
	lines                 []line
	shipFee, discount     int64
}

// create lên đơn bằng createOrder; Page (nếu có) do chủ bảng gán ngay sau đó. Trả mã đơn.
func (c *reportsRun) create(o orderSpec) string {
	c.t.Helper()
	items := make([]map[string]any, 0, len(o.lines))
	for _, l := range o.lines {
		items = append(items, map[string]any{"variantId": c.sku[l.sku], "qty": l.qty})
	}
	body := orderReq(c.sku[o.lines[0].sku], 1, o.shipFee)
	body["items"] = items
	body["channel"] = o.channel
	body["intent"] = o.intent
	if o.discount > 0 {
		body["orderDiscount"] = o.discount
	}
	if o.source != uuid.Nil {
		body["trafficSourceId"] = o.source
	}
	if o.care != uuid.Nil {
		body["careBy"] = o.care
	}
	got := c.mustCall(http.MethodPost, "/api/v1/orders", body, http.StatusCreated)
	id := str(got["id"])
	c.orders[o.name] = id
	if o.page != uuid.Nil {
		c.inTenant(func(tx pgx.Tx) error {
			_, err := tx.Exec(c.t.Context(), `UPDATE ord.orders SET channel_account_id = $1 WHERE id = $2`, o.page, id)
			return err //nolint:wrapcheck // test
		})
	}
	return id
}

func (c *reportsRun) orderVersion(orderID string) any {
	c.t.Helper()
	return c.mustCall(http.MethodGet, "/api/v1/orders/"+orderID, nil, http.StatusOK)["version"]
}

func (c *reportsRun) orderIs(orderID, status string) {
	c.t.Helper()
	waitFor(c.t, 3*stepBudget, "đơn "+status, func() bool {
		return c.mustCall(http.MethodGet, "/api/v1/orders/"+orderID, nil, http.StatusOK)["status"] == status
	})
}

// fakeOpMode đặt chế độ của một thao tác của hãng giả (POST /admin/mode {op, mode}).
func (c *reportsRun) fakeOpMode(op, mode string) {
	c.t.Helper()
	req, err := http.NewRequestWithContext(c.t.Context(), http.MethodPost, c.fc+"/admin/mode",
		strings.NewReader(`{"op":"`+op+`","mode":"`+mode+`"}`))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if status, body := call(c.t, req); status != http.StatusOK {
		c.t.Fatalf("hãng giả đặt chế độ %s/%s = %d %s", op, mode, status, body)
	}
}

// scenario dựng chín đơn của kế hoạch (Task 227) bằng API; hai mẫu đầu O1, O2... theo thứ tự ghi trong từng bước.
func (c *reportsRun) scenario() (p1, p2 uuid.UUID) {
	c.t.Helper()
	c.products()
	p1, p2 = c.page("P1"), c.page("P2")
	live, mes, web := c.source("Live trưa P1"), c.source("Mes P2"), c.source("Website")
	an, binh := c.staffUser("An"), c.staffUser("Bình")

	// O1: facebook P1, 2 × áo A-S, ship 30.000: lên → xác nhận → hãng lấy → giao xong.
	o1 := c.create(orderSpec{
		name: "O1", channel: "facebook", intent: "confirmed", page: p1, source: live, care: an,
		lines: []line{{"AO-A-S", 2}}, shipFee: 30000,
	})
	_, t1 := c.pickedUp(o1)
	c.advance(t1, fake.RawDelivered)
	c.orderIs(o1, "delivered")

	// O2: facebook P1, 1 × quần B: lên → xác nhận → hãng lấy → hoàn về → kho nhận.
	o2 := c.create(orderSpec{
		name: "O2", channel: "facebook", intent: "confirmed", page: p1, source: live, care: an,
		lines: []line{{"QUAN-B", 1}}, shipFee: 30000,
	})
	s2, t2 := c.pickedUp(o2)
	c.advance(t2, fake.RawReturning)
	c.shipmentIs(s2, "returning")
	c.advance(t2, fake.RawReturned)
	c.shipmentIs(s2, "returned")
	c.receiveReturn(o2, s2)

	// O3: facebook P2, 1 × áo A-M, miễn ship: vận đơn đầu hủy trước khi lấy (đổi hãng), vận đơn sau hãng đã lấy.
	o3 := c.create(orderSpec{
		name: "O3", channel: "facebook", intent: "confirmed", page: p2, source: mes, care: binh,
		lines: []line{{"AO-A-M", 1}},
	})
	first := str(c.mustCall(http.MethodPost, "/api/v1/orders/"+o3+"/shipments",
		map[string]any{"carrierAccountId": c.s.shop.Account}, http.StatusAccepted)["id"])
	c.awaitingTracking(first)
	if st, body, err := c.call(http.MethodPost, "/api/v1/shipments/"+first+"/cancel", map[string]any{"reason": "Đổi hãng"}); err != nil ||
		(st != http.StatusOK && st != http.StatusAccepted) {
		c.t.Fatalf("hủy vận đơn đầu của O3 = %d %v (%v)", st, body, err)
	}
	c.shipmentIs(first, "cancelled")
	c.pickedUp(o3)

	// O4: web, không Page, nguồn Website, chưa phân công, 2 × váy C, giảm 40.000, ship 30.000: chỉ lên (new).
	c.create(orderSpec{
		name: "O4", channel: "web", intent: "new", source: web,
		lines: []line{{"VAY-C", 2}}, shipFee: 30000, discount: 40000,
	})

	// O5: nhập tay, không Page, không nguồn, Bình, 1 × quần B: xác nhận, chưa gửi hãng.
	c.create(orderSpec{name: "O5", channel: "manual", intent: "confirmed", care: binh, lines: []line{{"QUAN-B", 1}}})

	// O6: facebook P2, 1 × váy C: lên rồi hủy.
	o6 := c.create(orderSpec{
		name: "O6", channel: "facebook", intent: "new", page: p2, source: mes, care: binh,
		lines: []line{{"VAY-C", 1}},
	})
	c.cancelNew(o6)

	// O7: facebook P1, 1 × áo A-S: nháp, chưa chốt.
	c.create(orderSpec{
		name: "O7", channel: "facebook", intent: "draft", page: p1, source: live, care: an,
		lines: []line{{"AO-A-S", 1}},
	})

	// O9 trước O8 (O8 dựng sau cùng vì hãng giả bị đặt chế độ chậm trả lời hủy): giao xong rồi khách trả 1 cái.
	o9 := c.create(orderSpec{
		name: "O9", channel: "facebook", intent: "confirmed", page: p1, source: live, care: an,
		lines: []line{{"AO-A-M", 3}}, shipFee: 30000,
	})
	_, t9 := c.pickedUp(o9)
	c.advance(t9, fake.RawDelivered)
	c.orderIs(o9, "delivered")
	c.customerReturn(o9)

	// O8: facebook P1, 1 × áo A-S, ship 30.000: nháp → xác nhận → gửi hãng (chờ lấy) → khách hủy; hãng giả trả 429
	// cho lệnh hủy nên đơn đứng ở "đang yêu cầu hủy" suốt bài thử.
	o8 := c.create(orderSpec{
		name: "O8", channel: "facebook", intent: "draft", page: p1, source: live, care: an,
		lines: []line{{"AO-A-S", 1}}, shipFee: 30000,
	})
	c.mustCall(http.MethodPost, "/api/v1/orders/"+o8+"/confirm", map[string]any{"version": c.orderVersion(o8)}, http.StatusOK)
	s8 := str(c.mustCall(http.MethodPost, "/api/v1/orders/"+o8+"/shipments",
		map[string]any{"carrierAccountId": c.s.shop.Account}, http.StatusAccepted)["id"])
	c.awaitingTracking(s8)
	c.fakeOpMode("cancel", "rate_limited")
	c.mustCall(http.MethodPost, "/api/v1/orders/"+o8+"/cancel",
		map[string]any{"version": c.orderVersion(o8), "reason": "customer_changed_mind"}, http.StatusOK)
	c.orderIs(o8, "cancel_requested")
	return p1, p2
}

// cancelNew hủy đơn mới (chưa gửi hãng) và chờ cancelled.
func (c *reportsRun) cancelNew(orderID string) {
	c.t.Helper()
	c.mustCall(http.MethodPost, "/api/v1/orders/"+orderID+"/cancel",
		map[string]any{"version": c.orderVersion(orderID), "reason": "customer_changed_mind"}, http.StatusOK)
	c.orderIs(orderID, "cancelled")
}

// customerReturn: khách trả 1 cái của đơn đã giao, kho nhận nhập lại bán được.
func (c *reportsRun) customerReturn(orderID string) {
	c.t.Helper()
	o := c.mustCall(http.MethodGet, "/api/v1/orders/"+orderID, nil, http.StatusOK)
	items, _ := o["items"].([]any)
	itemID := str(items[0].(map[string]any)["id"]) //nolint:forcetypeassert // test
	ret := c.mustCall(http.MethodPost, "/api/v1/orders/"+orderID+"/returns", map[string]any{
		"kind": "customer_return", "reason": "Không vừa", "lines": []map[string]any{{"orderItemId": itemID, "qty": 1}},
	}, http.StatusCreated)
	c.mustCall(http.MethodPost, "/api/v1/returns/"+str(ret["id"])+"/receive", map[string]any{
		"version": ret["version"], "lines": []map[string]any{{"orderItemId": itemID, "condition": "sellable", "sellable": 1}},
	}, http.StatusOK)
}

// --- gọi báo cáo -----------------------------------------------------------------------------------------------------

func (c *reportsRun) period() string { return "from=" + c.day + "&to=" + c.day }

// getAs gọi GET bằng thẻ token; trả thân JSON, đòi đúng mã want.
func (c *reportsRun) getAs(token, path string, want int) map[string]any {
	c.t.Helper()
	owner := c.s.token
	c.s.token = token // thẻ của người gọi; test chạy tuần tự
	defer func() { c.s.token = owner }()
	return c.mustCall(http.MethodGet, path, nil, want)
}

func (c *reportsRun) report(path string) map[string]any {
	c.t.Helper()
	m := c.getAs(c.s.token, path, http.StatusOK)
	if m["status"] != "ready" {
		c.t.Fatalf("GET %s: status = %v, muốn ready (báo cáo phải ra ngay tại chỗ)", path, m["status"])
	}
	return m
}

// rowsByName đổi mảng dòng (khóa key hay field) thành map theo tên trong golden (names) hoặc theo chính khóa.
func (c *reportsRun) rowsByName(rows any, field string) map[string]any {
	c.t.Helper()
	list, _ := rows.([]any)
	out := make(map[string]any, len(list))
	for _, it := range list {
		m, _ := it.(map[string]any)
		k := str(m[field])
		if n, ok := c.names[k]; ok {
			k = n
		}
		out[k] = m
	}
	return out
}

// --- so sánh -----------------------------------------------------------------------------------------------------------

// expectSubset đòi mọi khóa của want (golden) có trong got với giá trị bằng; chỉ so các khóa golden nêu.
func expectSubset(t *testing.T, what string, got, want any) {
	t.Helper()
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			t.Errorf("%s: %v, muốn đối tượng %v", what, got, w)
			return
		}
		for k, wv := range w {
			if strings.HasPrefix(k, "_") {
				continue
			}
			gv, has := g[k]
			if !has {
				t.Errorf("%s.%s: thiếu khóa (có %v)", what, k, g)
				continue
			}
			expectSubset(t, what+"."+k, gv, wv)
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			t.Errorf("%s: %v, muốn mảng %d phần tử %v", what, got, len(w), w)
			return
		}
		for i := range w {
			expectSubset(t, fmt.Sprintf("%s[%d]", what, i), g[i], w[i])
		}
	default:
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, muốn %v", what, got, want)
		}
	}
}

func loadGolden(t *testing.T) map[string]map[string]any {
	t.Helper()
	b, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var g map[string]any
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("%s: %v", goldenPath, err)
	}
	out := map[string]map[string]any{}
	for k, v := range g {
		if m, ok := v.(map[string]any); ok {
			out[k] = m
		}
	}
	return out
}

// countsOf đổi dòng của báo cáo đơn thành "tên dòng → số đơn".
func (c *reportsRun) countsOf(dim string) map[string]any {
	c.t.Helper()
	r := c.report("/api/v1/reports/sales/orders?" + c.period() + "&groupBy=" + dim)
	out := map[string]any{}
	for k, row := range c.rowsByName(r["rows"], "key") {
		out[k] = row.(map[string]any)["orders"] //nolint:forcetypeassert // test
	}
	return out
}

// --- câu đếm độc lập (lớp b) -------------------------------------------------------------------------------------

const vnDayStart = `(date_trunc('day', now() AT TIME ZONE 'Asia/Ho_Chi_Minh') AT TIME ZONE 'Asia/Ho_Chi_Minh')`

// placedIDs: đơn lên hôm nay = có dòng lịch sử sang new/confirmed từ rỗng/nháp (Task 227), đọc thẳng bảng gốc.
const placedIDs = `SELECT DISTINCT order_id FROM ord.order_status_history
	WHERE to_status IN ('new', 'confirmed') AND from_status IN ('', 'draft') AND occurred_at >= ` + vnDayStart

// rawCounts đếm trực tiếp trên bảng gốc, không qua view hay mã của reporting.
type rawCounts struct {
	placed, active, cancelled, delivered, returned    int64
	goodsActive                                       int64
	pendingConfirm, pendingShip, cancelRequested      int64
	custReturnLines                                   int64
	shipCreated, shipCancelled, shipFeeTotal, shipCOD int64
}

func (c *reportsRun) raw() rawCounts {
	c.t.Helper()
	var r rawCounts
	c.inTenant(func(tx pgx.Tx) error {
		ctx := c.t.Context()
		q := func(dst *int64, sql string) {
			if err := tx.QueryRow(ctx, sql).Scan(dst); err != nil {
				c.t.Fatalf("câu đếm %q: %v", sql, err)
			}
		}
		q(&r.placed, `SELECT count(*) FROM (`+placedIDs+`) p`)
		q(&r.active, `SELECT count(*) FROM ord.orders o WHERE o.status <> 'cancelled' AND o.id IN (`+placedIDs+`)`)
		q(&r.cancelled, `SELECT count(*) FROM ord.orders o WHERE o.status = 'cancelled' AND o.id IN (`+placedIDs+`)`)
		q(&r.delivered, `SELECT count(DISTINCT order_id) FROM ord.order_status_history WHERE to_status = 'delivered' AND occurred_at >= `+vnDayStart)
		q(&r.returned, `SELECT count(DISTINCT order_id) FROM ord.order_status_history WHERE to_status = 'returned' AND occurred_at >= `+vnDayStart)
		q(&r.goodsActive, `SELECT coalesce(sum(i.net_amount), 0)::bigint FROM ord.order_items i JOIN ord.orders o ON o.id = i.order_id
			WHERE o.status <> 'cancelled' AND o.id IN (`+placedIDs+`)`)
		q(&r.pendingConfirm, `SELECT count(*) FROM ord.orders WHERE status = 'new'`)
		q(&r.pendingShip, `SELECT count(*) FROM ord.orders o WHERE o.status = 'confirmed' AND NOT EXISTS (
			SELECT 1 FROM fulfillment.shipments s WHERE s.order_id = o.id AND s.status <> 'cancelled')`)
		q(&r.cancelRequested, `SELECT count(*) FROM ord.orders WHERE status = 'cancel_requested'`)
		q(&r.custReturnLines, `SELECT count(*) FROM ord.return_items ri JOIN ord.returns r ON r.id = ri.return_id
			WHERE r.kind = 'customer_return' AND r.received_at >= `+vnDayStart)
		q(&r.shipCreated, `SELECT count(*) FROM fulfillment.shipments WHERE status <> 'cancelled' AND created_at >= `+vnDayStart)
		q(&r.shipCancelled, `SELECT count(*) FROM fulfillment.shipments WHERE status = 'cancelled' AND created_at >= `+vnDayStart)
		q(&r.shipFeeTotal, `SELECT coalesce(sum(coalesce(fee_actual, fee, 0)), 0)::bigint FROM fulfillment.shipments
			WHERE status <> 'cancelled' AND created_at >= `+vnDayStart)
		q(&r.shipCOD, `SELECT coalesce(sum(cod_amount), 0)::bigint FROM fulfillment.shipments
			WHERE status <> 'cancelled' AND created_at >= `+vnDayStart)
		return nil
	})
	return r
}

func num(v any) int64 {
	f, _ := v.(float64)
	return int64(f)
}

func at(m map[string]any, path ...string) map[string]any {
	cur := m
	for _, p := range path {
		cur, _ = cur[p].(map[string]any)
	}
	return cur
}

// --- test ---------------------------------------------------------------------------------------------------------------

// Lớp (a): mọi số của năm báo cáo bằng golden; người chỉ có Page P1 thấy 4 đơn.
func TestReportsGolden(t *testing.T) {
	// Không t.Parallel (Task 484): newStack đặt tracer provider của cả tiến trình.
	c := newReportsRun(t)
	p1, _ := c.scenario()
	g := loadGolden(t)
	q := c.period()

	// BC01 tổng quan.
	dash := c.report("/api/v1/reports/sales/dashboard?" + q)
	want := maps.Clone(g["dashboard"])
	byChannel := c.rowsByName(dash["byChannel"], "channel")
	expectSubset(t, "tổng quan.byChannel", byChannel, want["byChannel"])
	delete(want, "byChannel")
	expectSubset(t, "tổng quan", dash, want)
	if n := len(dash["byDay"].([]any)); n != 0 { //nolint:forcetypeassert // hợp đồng: mảng
		t.Errorf("kỳ một ngày: byDay có %d dòng, muốn 0", n)
	}

	// Người chỉ có report.sales.read trên P1.
	c.staff.Grant(t, c.s.tenant, c.staff.User(t, c.s.tenant, "p1@elimaz.test", "active"),
		[]string{"report.sales.read"}, "page", p1.String())
	p1Token := c.s.keys.Sign(cfaccesstest.AccessClaims{Email: "p1@elimaz.test", AUD: stackAUD, MFA: []string{"mfa"}})
	p1Dash := c.getAs(p1Token, "/api/v1/reports/sales/dashboard?"+q, http.StatusOK)
	expectSubset(t, "tổng quan của người chỉ có P1", p1Dash, g["dashboardP1"])

	// BC02 báo cáo đơn.
	byCare := c.report("/api/v1/reports/sales/orders?" + q + "&groupBy=care_by")
	expectSubset(t, "đơn theo nhân viên.rows", c.rowsByName(byCare["rows"], "key"), g["ordersCareBy"]["rows"])
	expectSubset(t, "đơn theo nhân viên.totals", byCare["totals"], g["ordersCareBy"]["totals"])
	if n := len(byCare["rows"].([]any)); n != 3 { //nolint:forcetypeassert // hợp đồng: mảng
		t.Errorf("số dòng nhân viên = %d, muốn 3", n)
	}
	counts := g["ordersCounts"]
	for _, dim := range []struct{ param, golden string }{
		{"source", "source"}, {"carrier", "carrier"}, {"channel", "channel"}, {"page", "page"}, {"status", "status"},
	} {
		expectSubset(t, "số đơn theo "+dim.param, c.countsOf(dim.param), counts[dim.golden])
		if got, want := len(c.countsOf(dim.param)), len(counts[dim.golden].(map[string]any)); got != want { //nolint:forcetypeassert // golden
			t.Errorf("số dòng theo %s = %d, muốn %d", dim.param, got, want)
		}
	}
	done := c.report("/api/v1/reports/sales/orders?" + q + "&groupBy=care_by&basis=completed")
	expectSubset(t, "đơn theo ngày hoàn tất.totals", done["totals"], g["ordersCompleted"]["totals"])
	expectSubset(t, "đơn theo ngày hoàn tất.rows", c.rowsByName(done["rows"], "key"), g["ordersCompleted"]["rows"])

	// Báo cáo hãng.
	car := c.report("/api/v1/reports/sales/carriers?" + q)
	expectSubset(t, "hãng.rows", c.rowsByName(car["rows"], "carrierKey"), g["carriers"]["rows"])
	expectSubset(t, "hãng.totals", car["totals"], g["carriers"]["totals"])

	// Bán chạy.
	best := func(query string) any { return c.report("/api/v1/reports/sales/best-sellers?" + q + query)["rows"] }
	bs := g["bestSellers"]
	expectSubset(t, "bán chạy sku đơn lên", best("&level=sku&basis=placed"), bs["skuPlaced"])
	expectSubset(t, "bán chạy mẫu đơn lên", best("&level=product&basis=placed"), bs["productPlaced"])
	expectSubset(t, "bán chạy mẫu đơn thành công", best("&level=product&basis=delivered"), bs["productDelivered"])
	expectSubset(t, "bán chạy sku đơn thành công", best("&level=sku&basis=delivered"), bs["skuDelivered"])
}

// Lớp (b) và (c): số báo cáo bằng câu đếm độc lập trên bảng gốc; số "chờ" bằng danh sách đơn của M2; mỗi ô của báo
// cáo đơn bằng total của danh sách chi tiết; tồn hôm nay khớp bảng số dư.
func TestReportsGolden_MatchIndependentCounts(t *testing.T) {
	c := newReportsRun(t)
	c.scenario()
	q := c.period()
	raw := c.raw()
	dash := c.report("/api/v1/reports/sales/dashboard?" + q)

	// (b) câu đếm độc lập trên bảng gốc.
	for _, x := range []struct {
		what      string
		got, want int64
	}{
		{"đơn lên", num(at(dash, "placed")["orders"]), raw.placed},
		{"đơn chưa hủy", num(at(dash, "active")["orders"]), raw.active},
		{"doanh số chưa hủy", num(at(dash, "active")["goodsAmount"]), raw.goodsActive},
		{"đơn hủy", num(at(dash, "cancelled")["orders"]), raw.cancelled},
		{"đơn giao xong", num(at(dash, "delivered")["orders"]), raw.delivered},
		{"đơn hoàn", num(at(dash, "returned")["orders"]), raw.returned},
		{"chờ xác nhận", num(at(dash, "pending")["confirm"]), raw.pendingConfirm},
		{"chờ gửi hãng", num(at(dash, "pending")["ship"]), raw.pendingShip},
		{"đang yêu cầu hủy", num(at(dash, "pending")["cancelRequested"]), raw.cancelRequested},
		{"dòng hàng khách trả", num(at(dash, "customerReturns")["items"]), raw.custReturnLines},
	} {
		if x.got != x.want {
			t.Errorf("%s: báo cáo %d, câu đếm độc lập %d", x.what, x.got, x.want)
		}
	}
	car := c.report("/api/v1/reports/sales/carriers?" + q)
	for _, x := range []struct {
		what      string
		got, want int64
	}{
		{"vận đơn tạo (chưa hủy)", num(at(car, "totals")["created"]), raw.shipCreated},
		{"vận đơn hủy", num(at(car, "totals")["cancelled"]), raw.shipCancelled},
		{"phí hãng", num(at(car, "totals")["feeTotal"]), raw.shipFeeTotal},
		{"COD", num(at(car, "totals")["codTotal"]), raw.shipCOD},
	} {
		if x.got != x.want {
			t.Errorf("%s: báo cáo %d, câu đếm độc lập %d", x.what, x.got, x.want)
		}
	}

	c.checkPendingMatchesList(t, dash)

	// Mỗi số của tổng quan = total của danh sách chi tiết cùng metric.
	for _, m := range []struct {
		metric string
		got    int64
	}{
		{"placed", num(at(dash, "placed")["orders"])},
		{"placed_active", num(at(dash, "active")["orders"])},
		{"cancelled", num(at(dash, "cancelled")["orders"])},
		{"delivered", num(at(dash, "delivered")["orders"])},
		{"returned", num(at(dash, "returned")["orders"])},
		{"customer_returned", num(at(dash, "customerReturns")["items"])},
		{"pending_confirm", num(at(dash, "pending")["confirm"])},
		{"pending_ship", num(at(dash, "pending")["ship"])},
		{"cancel_requested", num(at(dash, "pending")["cancelRequested"])},
	} {
		d := c.report("/api/v1/reports/sales/orders/drilldown?" + q + "&metric=" + m.metric)
		if n := num(d["total"]); n != m.got {
			t.Errorf("tổng quan %s = %d, danh sách chi tiết total = %d", m.metric, m.got, n)
		}
	}

	c.checkCellsMatchDrilldown(t, q)
	c.checkStockMatchesBalance(t)
}

// checkPendingMatchesList (c): số chờ của tổng quan = số dòng của danh sách đơn M2 cùng điều kiện.
func (c *reportsRun) checkPendingMatchesList(t *testing.T, dash map[string]any) {
	t.Helper()
	listed := func(query string) int64 {
		page := c.mustCall(http.MethodGet, "/api/v1/orders?"+query+"&limit=100", nil, http.StatusOK)
		items, _ := page["items"].([]any)
		if page["nextCursor"] != nil && page["nextCursor"] != "" {
			t.Fatalf("danh sách %s còn trang sau, không đếm được bằng số dòng", query)
		}
		return int64(len(items))
	}
	for _, x := range []struct {
		what, query string
		got         int64
	}{
		{"chờ xác nhận", "status=new", num(at(dash, "pending")["confirm"])},
		{"chờ gửi hãng", "label=confirmed", num(at(dash, "pending")["ship"])},
		{"đang yêu cầu hủy", "status=cancel_requested", num(at(dash, "pending")["cancelRequested"])},
	} {
		if n := listed(x.query); n != x.got {
			t.Errorf("%s: tổng quan %d, danh sách đơn ?%s %d dòng", x.what, x.got, x.query, n)
		}
	}
}

// checkCellsMatchDrilldown: mỗi ô của báo cáo đơn theo nhân viên (cột "Số đơn" = đơn lên) = total của danh sách chi tiết của ô đó.
func (c *reportsRun) checkCellsMatchDrilldown(t *testing.T, q string) {
	t.Helper()
	byCare := c.report("/api/v1/reports/sales/orders?" + q + "&groupBy=care_by")
	rows, _ := byCare["rows"].([]any)
	for _, it := range rows {
		row, _ := it.(map[string]any)
		d := c.report("/api/v1/reports/sales/orders/drilldown?" + q + "&metric=placed&cellDimension=care_by&cellKey=" +
			url.QueryEscape(str(row["key"])))
		if n := num(d["total"]); n != num(row["orders"]) {
			t.Errorf("ô %v: báo cáo %d, danh sách chi tiết total %d", row["key"], num(row["orders"]), n)
		}
	}
}

// checkStockMatchesBalance: tồn áo A-S hôm nay ở getInventoryStock = on_hand − damaged − held − in_transit của bảng số dư.
func (c *reportsRun) checkStockMatchesBalance(t *testing.T) {
	t.Helper()
	var onHand, held, inTransit, damaged int64
	c.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(c.t.Context(), `SELECT on_hand, held, in_transit, damaged FROM inventory.stock_balances
			WHERE variant_id = $1 AND warehouse_id = $2`, c.sku["AO-A-S"], c.s.shop.Warehouse).Scan(&onHand, &held, &inTransit, &damaged)
	})
	stock := c.report("/api/v1/reports/inventory/stock?q=AO-A-S")
	srows, _ := stock["rows"].([]any)
	if len(srows) != 1 {
		t.Fatalf("tồn theo tiền tố AO-A-S = %v, muốn 1 dòng", stock["rows"])
	}
	row := srows[0].(map[string]any) //nolint:forcetypeassert // test
	if num(row["onHand"]) != onHand || num(row["held"]) != held || num(row["inTransit"]) != inTransit ||
		num(row["damaged"]) != damaged || num(row["available"]) != onHand-damaged-held-inTransit {
		t.Errorf("tồn A-S báo cáo %v, bảng số dư on_hand %d held %d in_transit %d damaged %d", row, onHand, held, inTransit, damaged)
	}
}

// Xuất Excel báo cáo đơn qua POST …/runs → worker chạy → tải tệp qua …/file → đọc lại tệp: số bằng JSON của GET.
func TestReportsGolden_ExcelExportMatchesJSON(t *testing.T) {
	c := newReportsRun(t)
	c.scenario()
	params := map[string]any{"from": c.day, "to": c.day, "groupBy": "care_by"}
	run := c.mustCall(http.MethodPost, "/api/v1/reports/sales/runs",
		map[string]any{"report": "orders", "format": "xlsx", "params": params}, http.StatusAccepted)
	runID := str(run["id"])
	waitFor(t, 6*stepBudget, "lần chạy xuất Excel ready", func() bool {
		got := c.mustCall(http.MethodGet, "/api/v1/reports/sales/runs/"+runID, nil, http.StatusOK)
		if got["status"] == "failed" {
			t.Fatalf("lần chạy xuất Excel lỗi: %v", got)
		}
		return got["status"] == "ready"
	})
	link := c.mustCall(http.MethodGet, "/api/v1/reports/sales/runs/"+runID+"/file", nil, http.StatusOK)
	resp, err := http.Get(str(link["url"])) //nolint:noctx // test, máy chủ MemStore cục bộ
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("tải tệp = %d %v", resp.StatusCode, err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("mở tệp xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()
	sheet := f.GetSheetName(0)
	table, err := f.GetRows(sheet)
	if err != nil {
		t.Fatal(err)
	}
	// Tìm dòng tiêu đề (ô thứ hai là "Số đơn"), các dòng sau: nhân viên rồi dòng tổng.
	head := slices.IndexFunc(table, func(r []string) bool { return len(r) > 1 && r[1] == "Số đơn" })
	if head < 0 {
		t.Fatalf("tệp không có dòng tiêu đề 'Số đơn': %v", table)
	}
	got := map[string]int64{}
	for _, r := range table[head+1:] {
		if len(r) < 2 || r[0] == "" {
			continue
		}
		n, err := strconv.ParseInt(strings.NewReplacer(".", "", ",", "").Replace(r[1]), 10, 64)
		if err != nil {
			continue // dòng ghi chú cuối tệp
		}
		got[r[0]] = n
	}
	rep := c.report("/api/v1/reports/sales/orders?" + c.period() + "&groupBy=care_by")
	want := map[string]int64{str(at(rep, "totals")["label"]): num(at(rep, "totals")["orders"])}
	for _, it := range rep["rows"].([]any) { //nolint:forcetypeassert // hợp đồng: mảng
		row := it.(map[string]any) //nolint:forcetypeassert // hợp đồng
		want[str(row["label"])] = num(row["orders"])
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tệp Excel (nhãn → số đơn) = %v, JSON của báo cáo %v", got, want)
	}
	if want["Tổng"] != 8 {
		t.Errorf("dòng tổng = %v, muốn 8 đơn lên", want)
	}
}
