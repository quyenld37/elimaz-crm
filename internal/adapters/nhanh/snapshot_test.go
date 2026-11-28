package nhanh_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/nhanh"
)

// fakeNhanh là máy Nhanh giả: 120 mẫu đang bán (id 1000..1119, mẫu chẵn có 2 biến thể), 250 khách (dữ liệu bịa), phân
// trang theo next {"id":…}. Ghi lại mọi đường đã gọi.
type fakeNhanh struct {
	mu          sync.Mutex
	paths       []string
	limitOnce   bool // lời gọi customer/list đầu tiên trả ERR_429
	limited     bool
	noContent   map[int64]bool // product/detail thiếu content
	custNoPhone map[int]bool
}

type reqBody struct {
	Filters   map[string]json.RawMessage `json:"filters"`
	Paginator struct {
		Size int             `json:"size"`
		Next json.RawMessage `json:"next"`
	} `json:"paginator"`
}

func (f *fakeNhanh) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/")
	f.paths = append(f.paths, path)
	var b reqBody
	_ = json.NewDecoder(r.Body).Decode(&b)
	switch path {
	case "product/list":
		f.productList(w, b)
	case "product/detail":
		var id int64
		_ = json.Unmarshal(b.Filters["id"], &id)
		d := map[string]any{"id": id, "name": fmt.Sprintf("Mẫu %d", id), "description": "mô tả", "content": "<p>nội dung</p>"}
		if f.noContent[id] {
			delete(d, "content")
		}
		reply(w, d, nil)
	case "product/inventory":
		var ids []int64
		_ = json.Unmarshal(b.Filters["ids"], &ids)
		rows := []any{}
		for _, id := range ids {
			rows = append(rows, map[string]any{"productId": id, "inventory": map[string]any{"remain": id % 7, "available": id % 5}})
		}
		reply(w, rows, nil)
	case "customer/list":
		if f.limitOnce && !f.limited {
			f.limited = true
			_, _ = w.Write([]byte(`{"code":0,"errorCode":"ERR_429","data":{"lockedSeconds":5,"unlockedAt":1000005}}`))
			return
		}
		f.customerList(w, b)
	default:
		http.NotFound(w, r)
	}
}

func nextID(b reqBody) int {
	var n struct{ ID int }
	_ = json.Unmarshal(b.Paginator.Next, &n)
	return n.ID
}

func reply(w http.ResponseWriter, data any, next map[string]int) {
	out := map[string]any{"code": 1, "data": data}
	if next != nil {
		out["paginator"] = map[string]any{"next": next}
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (f *fakeNhanh) productList(w http.ResponseWriter, b reqBody) {
	size := min(b.Paginator.Size, 100)
	if raw, ok := b.Filters["parentId"]; ok {
		var pid int64
		_ = json.Unmarshal(raw, &pid)
		reply(w, []any{
			map[string]any{"id": pid*10 + 1, "parentId": pid, "name": "đỏ/S"},
			map[string]any{"id": pid*10 + 2, "parentId": pid, "name": "đỏ/M"},
		}, nil)
		return
	}
	var all []int64
	if raw, ok := b.Filters["ids"]; ok {
		_ = json.Unmarshal(raw, &all)
	} else {
		for id := int64(1119); id >= 1000; id-- { // mới nhất trước (sort id desc)
			all = append(all, id)
		}
	}
	start := nextID(b) // next {"id": vị trí}
	end := min(start+size, len(all))
	rows := []any{}
	for _, id := range all[start:end] {
		pid := int64(-1)
		if id%2 == 0 {
			pid = -2
		}
		rows = append(rows, map[string]any{"id": id, "parentId": pid, "status": 2, "name": fmt.Sprintf("Mẫu %d", id)})
	}
	var next map[string]int
	if end < len(all) {
		next = map[string]int{"id": end}
	}
	reply(w, rows, next)
}

func (f *fakeNhanh) customerList(w http.ResponseWriter, b reqBody) {
	size := min(b.Paginator.Size, 100)
	start := nextID(b)
	end := min(start+size, 250)
	rows := []any{}
	for i := start; i < end; i++ {
		c := map[string]any{"id": 5000 + i, "name": fmt.Sprintf("Khách giả %d", i), "mobile": fmt.Sprintf("09000%05d", i)}
		if f.custNoPhone[i] {
			delete(c, "mobile")
		}
		rows = append(rows, c)
	}
	var next map[string]int
	if end < 250 {
		next = map[string]int{"id": end}
	}
	reply(w, rows, next)
}

func (f *fakeNhanh) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(slices.DeleteFunc(slices.Clone(f.paths), func(p string) bool { return p != path }))
}

func TestCollect_DefaultFiftyNewestWithVariantsDetailStock(t *testing.T) {
	t.Parallel()
	f := &fakeNhanh{noContent: map[int64]bool{1118: true}, custNoPhone: map[int]bool{3: true, 4: true}}
	c, _ := newTestClient(t, f)
	snap, err := nhanh.Collect(t.Context(), c, nhanh.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Products) != 50 {
		t.Fatalf("số mẫu = %d, muốn 50", len(snap.Products))
	}
	if snap.Products[0].ID != 1119 || snap.Products[49].ID != 1070 {
		t.Errorf("mẫu đầu/cuối = %d/%d, muốn 1119/1070 (mới nhất trước)", snap.Products[0].ID, snap.Products[49].ID)
	}
	p := snap.Products[1] // 1118 chẵn: mẫu cha có 2 biến thể, thiếu content
	if p.ID != 1118 || len(p.Variants) != 2 || len(p.Inventory) != 3 || len(p.Detail) == 0 {
		t.Errorf("mẫu 1118: biến thể %d, tồn %d, chi tiết %d byte", len(p.Variants), len(p.Inventory), len(p.Detail))
	}
	if len(snap.Customers) != 250 {
		t.Errorf("số khách = %d, muốn 250 (qua 3 trang)", len(snap.Customers))
	}
	want := []nhanh.Gap{
		{Kind: "product_detail", ID: 1118, Field: "content"},
		{Kind: "customer", ID: 5003, Field: "mobile"},
		{Kind: "customer", ID: 5004, Field: "mobile"},
	}
	if !slices.Equal(snap.Gaps, want) {
		t.Errorf("gaps = %+v, muốn %+v", snap.Gaps, want)
	}
	// Chỉ đường đọc trong danh sách trắng được gọi.
	for _, p := range f.paths {
		if !slices.Contains(nhanh.ReadPaths(), p) {
			t.Errorf("đã gọi đường %q", p)
		}
	}
	if f.count("product/detail") != 50 {
		t.Errorf("product/detail %d lần, muốn 50", f.count("product/detail"))
	}
}

// Hạn cứng 50: yêu cầu hơn thế lỗi trước khi gọi bất kỳ đường nào.
func TestCollect_HardLimitFifty(t *testing.T) {
	t.Parallel()
	f := &fakeNhanh{}
	c, _ := newTestClient(t, f)
	ids := make([]int64, 51)
	for i := range ids {
		ids[i] = int64(1000 + i)
	}
	for name, o := range map[string]nhanh.Options{"limit 51": {Limit: 51}, "51 mã": {ProductIDs: ids}, "limit âm": {Limit: -1}} {
		_, err := nhanh.Collect(t.Context(), c, o)
		if !errors.Is(err, nhanh.ErrBadOptions) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(f.paths) != 0 {
		t.Fatalf("đã gọi %v trước khi kiểm tham số", f.paths)
	}
}

// Danh sách mã tự chọn: chỉ các mẫu đó, theo thứ tự đã đưa.
func TestCollect_ExplicitProductIDs(t *testing.T) {
	t.Parallel()
	f := &fakeNhanh{}
	c, _ := newTestClient(t, f)
	snap, err := nhanh.Collect(t.Context(), c, nhanh.Options{ProductIDs: []int64{1003, 1004}, SkipCustomers: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Products) != 2 || snap.Products[0].ID != 1003 || snap.Products[1].ID != 1004 || len(snap.Products[1].Variants) != 2 {
		t.Fatalf("products = %+v", snap.Products)
	}
	if len(snap.Customers) != 0 || f.count("customer/list") != 0 {
		t.Errorf("SkipCustomers vẫn gọi customer/list")
	}
}

// ERR_429 giữa chừng: chờ rồi lấy đủ khách, không mất trang.
func TestCollect_SurvivesRateLimit(t *testing.T) {
	t.Parallel()
	f := &fakeNhanh{limitOnce: true}
	c, clk := newTestClient(t, f)
	start := clk.Now()
	snap, err := nhanh.Collect(t.Context(), c, nhanh.Options{Limit: 1})
	if err != nil || len(snap.Customers) != 250 {
		t.Fatalf("khách %d, lỗi %v", len(snap.Customers), err)
	}
	if clk.Now().Sub(start) < 5*time.Second {
		t.Errorf("không chờ tới unlockedAt")
	}
}

// Con trỏ trang không tiến (lặp mãi): dừng với lỗi, không vòng vô hạn.
func TestCollect_StuckPaginatorFails(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":1,"data":[{"id":1}],"paginator":{"next":{"id":1}}}`))
	}))
	if _, err := nhanh.Collect(t.Context(), c, nhanh.Options{ProductIDs: []int64{1}}); err == nil {
		// product/list theo mã không phân trang; customer/list mới lặp
		t.Fatal("muốn lỗi khi customer/list lặp con trỏ")
	}
}

// --since: bộ lọc updatedAtFrom (giây Unix) đi tới product/list (chọn mặc định) và customer/list.
func TestCollect_SinceSendsUpdatedAtFrom(t *testing.T) {
	t.Parallel()
	var got []string
	var mu sync.Mutex
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b reqBody
		_ = json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		got = append(got, r.URL.Path+" "+string(b.Filters["updatedAtFrom"]))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"code":1,"data":[]}`))
	}))
	since := time.Unix(1_700_000_000, 0)
	if _, err := nhanh.Collect(t.Context(), c, nhanh.Options{Since: since}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"/product/list 1700000000", "/customer/list 1700000000"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, muốn %v", got, want)
	}
}
