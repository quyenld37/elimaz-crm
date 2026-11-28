package nhanh_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/nhanh"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

// Dữ liệu giả hoàn toàn (không có dữ liệu thật của khách hay shop).
func fakeProduct() nhanh.Product {
	return nhanh.Product{
		ID:   100,
		List: raw(`{"id":100,"parentId":-2,"code":"ao-so-mi 01","name":"Áo sơ mi mẫu","categoryId":"55"}`),
		Detail: raw(`{"id":100,"name":"Áo sơ mi mẫu","description":"Vải thoáng mát","categoryId":55,` +
			`"images":{"avatar":"https://cdn.example.test/p100.jpg","others":["https://cdn.example.test/p100b.jpg","http://x.test/insecure.jpg"]}}`),
		Variants: []json.RawMessage{
			raw(`{"id":101,"parentId":100,"code":"AO01-DO-M","barcode":"8938500000011","prices":{"retail":"300000","retailVat":"30000","import":"150000"},` +
				`"attributes":[{"name":"Màu","value":"Đỏ"},{"name":"Size","value":"M"}],"images":{"avatar":"https://cdn.example.test/red.jpg"}}`),
			raw(`{"id":102,"parentId":100,"code":"AO01-DO-L","barcode":"8938500000011","prices":{"retail":300000,"retailVat":30000},` +
				`"attributes":[{"name":"màu sắc","value":"Đỏ"},{"name":"Kích cỡ","value":"L"}]}`),
			raw(`{"id":103,"parentId":100,"code":"AO01-DO-M","priceVatMode":1,"prices":{"retail":250000,"import":100000},` +
				`"attributes":[{"name":"Màu","value":"Xanh"},{"name":"Size","value":"S"}],"images":{"avatar":"https://cdn.example.test/blue.jpg"}}`),
		},
		Inventory: []json.RawMessage{
			raw(`{"productId":101,"inventory":{"remain":7,"available":5}}`),
			raw(`{"productId":102,"inventory":{"available":"3"}}`),
			raw(`{"productId":103,"inventory":{"remain":-4}}`),
		},
	}
}

// Biến thể: giá = bán lẻ + VAT (chế độ 2), VAT đã gồm (chế độ 1) thì không cộng; màu, size theo tên thuộc tính; tồn
// "remain" rồi "available", âm kẹp về 0; SKU trùng thêm mã Nhanh; mã vạch trùng bỏ; ảnh chung và ảnh theo màu.
func TestBuildDrafts_VariantsPricesStockImages(t *testing.T) {
	t.Parallel()
	ds := nhanh.BuildDrafts([]nhanh.Product{fakeProduct()})
	if len(ds) != 1 {
		t.Fatalf("drafts = %d", len(ds))
	}
	d := ds[0]
	if d.Code != "AO-SO-MI-01" || d.Name != "Áo sơ mi mẫu" || d.Description != "Vải thoáng mát" || d.CategoryLegacyID != 55 {
		t.Errorf("mẫu = %+v", d)
	}
	if len(d.Variants) != 3 {
		t.Fatalf("variants = %d", len(d.Variants))
	}
	v1, v2, v3 := d.Variants[0], d.Variants[1], d.Variants[2]
	if v1.SKU != "AO01-DO-M" || v1.Price != 330000 || v1.CostPrice == nil || *v1.CostPrice != 150000 || v1.Color != "Đỏ" || v1.Size != "M" ||
		v1.Stock == nil || *v1.Stock != 7 || v1.Barcode != "8938500000011" {
		t.Errorf("v1 = %+v", v1)
	}
	if v2.Price != 330000 || v2.CostPrice != nil || v2.Stock == nil || *v2.Stock != 3 || v2.Size != "L" || v2.Barcode != "" {
		t.Errorf("v2 = %+v (barcode trùng phải bỏ, thiếu giá vốn là nil)", v2)
	}
	if v3.SKU != "AO01-DO-M-103" || v3.Price != 250000 || v3.Stock == nil || *v3.Stock != 0 {
		t.Errorf("v3 = %+v (SKU trùng thêm mã Nhanh; VAT đã gồm; tồn âm về 0)", v3)
	}
	want := []nhanh.DraftImage{
		{URL: "https://cdn.example.test/p100.jpg"},
		{URL: "https://cdn.example.test/p100b.jpg"},
		{URL: "https://cdn.example.test/red.jpg", Color: "Đỏ"},
		{URL: "https://cdn.example.test/blue.jpg", Color: "Xanh"},
	}
	if !slices.Equal(d.Images, want) {
		t.Errorf("ảnh = %+v, muốn %+v (http bị bỏ, ảnh biến thể gắn màu)", d.Images, want)
	}
}

// Sản phẩm đơn: chính nó là biến thể; thiếu màu, size, mã, tên, giá → mặc định và Problem; Problem không chứa giá trị.
func TestBuildDrafts_SingleProductGapsBecomeProblems(t *testing.T) {
	t.Parallel()
	ds := nhanh.BuildDrafts([]nhanh.Product{{ID: 9, List: raw(`{"id":9}`), Variants: []json.RawMessage{}, Inventory: []json.RawMessage{}}})
	d := ds[0]
	if len(d.Variants) != 1 || d.Variants[0].LegacyID != 9 || d.Variants[0].Color != "Mặc định" || d.Variants[0].Size != "Free" ||
		d.Variants[0].SKU != "NHANH-9" || d.Code != "NHANH-9" || d.Name != "Sản phẩm Nhanh 9" || d.Variants[0].Price != 0 {
		t.Fatalf("draft = %+v", d)
	}
	fields := map[string]bool{}
	for _, p := range d.Problems {
		fields[p.Field] = true
		if p.LegacyID != 9 {
			t.Errorf("problem %+v sai mã", p)
		}
	}
	for _, f := range []string{"name", "description", "category", "code", "sku", "color", "size", "price", "cost_price", "stock", "images"} {
		if !fields[f] {
			t.Errorf("thiếu Problem %q (có %v)", f, fields)
		}
	}
}

// Mã không hợp khuôn (dấu, ký tự lạ, quá dài) được chuẩn hóa đúng khuôn của catalog.
func TestBuildDrafts_CodeShape(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 90)
	ds := nhanh.BuildDrafts([]nhanh.Product{{ID: 1, List: raw(`{"id":1,"code":"` + long + `"}`)}, {ID: 2, List: raw(`{"id":2,"code":"__x y"}`)}})
	if len(ds[0].Code) != 64 || strings.ToUpper(ds[0].Code) != ds[0].Code {
		t.Errorf("mã dài = %q", ds[0].Code)
	}
	if ds[1].Code != "X-Y" {
		t.Errorf("mã = %q, muốn X-Y", ds[1].Code)
	}
}

func TestSelect(t *testing.T) {
	t.Parallel()
	ps := []nhanh.Product{{ID: 1}, {ID: 2}, {ID: 3}}
	ids := func(p []nhanh.Product) []int64 {
		var o []int64
		for _, x := range p {
			o = append(o, x.ID)
		}
		return o
	}
	if got, err := nhanh.Select(ps, nil, nil); err != nil || !slices.Equal(ids(got), []int64{1, 2, 3}) {
		t.Errorf("mặc định = %v, %v", ids(got), err)
	}
	if got, err := nhanh.Select(ps, nil, []int64{2}); err != nil || !slices.Equal(ids(got), []int64{1, 3}) {
		t.Errorf("bỏ 2 = %v, %v", ids(got), err)
	}
	if got, err := nhanh.Select(ps, []int64{3, 1, 3}, nil); err != nil || !slices.Equal(ids(got), []int64{3, 1}) {
		t.Errorf("chỉ 3,1 = %v, %v", ids(got), err)
	}
	for name, c := range map[string]struct{ only, remove []int64 }{
		"mã lạ":    {[]int64{9}, nil},
		"bỏ mã lạ": {nil, []int64{9}},
		"bỏ hết":   {nil, []int64{1, 2, 3}},
	} {
		if _, err := nhanh.Select(ps, c.only, c.remove); !errors.Is(err, nhanh.ErrSelection) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// Hạn cứng 50 mẫu (kể cả khi bản chụp tự sửa tay có 51 mẫu).
func TestSelect_HardLimitFifty(t *testing.T) {
	t.Parallel()
	var ps []nhanh.Product
	for i := range 51 {
		ps = append(ps, nhanh.Product{ID: int64(i + 1)})
	}
	if _, err := nhanh.Select(ps, nil, nil); !errors.Is(err, nhanh.ErrSelection) {
		t.Fatalf("51 mẫu: err = %v", err)
	}
	if got, err := nhanh.Select(ps, nil, []int64{1}); err != nil || len(got) != 50 {
		t.Fatalf("bỏ một còn 50: %d, %v", len(got), err)
	}
}

func TestReadSnapshot_LatestAndVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := nhanh.LatestSnapshot(dir); !errors.Is(err, nhanh.ErrNoSnapshot) {
		t.Fatalf("thư mục rỗng: %v", err)
	}
	if _, err := nhanh.LatestSnapshot(filepath.Join(dir, "khong-co")); !errors.Is(err, nhanh.ErrNoSnapshot) {
		t.Fatalf("thư mục chưa có: %v", err)
	}
	for _, at := range []time.Time{time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC), time.Date(2026, 12, 2, 3, 0, 0, 0, time.UTC)} {
		if _, err := nhanh.WriteSnapshot(dir, &nhanh.Snapshot{Version: 1, TakenAt: at, Products: []nhanh.Product{fakeProduct()}}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := nhanh.LatestSnapshot(dir)
	if err != nil || !strings.HasSuffix(p, "20261202T030000Z.json") {
		t.Fatalf("mới nhất = %s, %v", p, err)
	}
	s, err := nhanh.ReadSnapshot(p)
	if err != nil || len(s.Products) != 1 || s.Products[0].ID != 100 {
		t.Fatalf("đọc = %+v, %v", s, err)
	}
	bad := filepath.Join(dir, "nhanh-snapshot-20270101T000000Z.json")
	if err := os.WriteFile(bad, []byte(`{"version":9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := nhanh.ReadSnapshot(bad); !errors.Is(err, nhanh.ErrSnapshotVersion) {
		t.Fatalf("phiên bản lạ: %v", err)
	}
}

var jpegHead = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F'}

func TestImageFetcher(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.jpg":
			_, _ = w.Write(jpegHead)
		case "/text":
			_, _ = w.Write([]byte("<html>"))
		case "/big":
			_, _ = w.Write(append(append([]byte{}, jpegHead...), make([]byte, nhanh.MaxImageBytes)...))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := nhanh.ImageFetcher{AllowPrivate: true}
	data, ct, err := f.Fetch(context.Background(), srv.URL+"/ok.jpg")
	if err != nil || ct != "image/jpeg" || len(data) != len(jpegHead) {
		t.Fatalf("ok: %d %s %v", len(data), ct, err)
	}
	for path, want := range map[string]error{"/text": nhanh.ErrImageType, "/big": nhanh.ErrImageTooLarge, "/none": nhanh.ErrImageStatus} {
		if _, _, err := f.Fetch(context.Background(), srv.URL+path); !errors.Is(err, want) {
			t.Errorf("%s: err = %v, muốn %v", path, err, want)
		}
	}
}

// Mặc định chặn http và địa chỉ nội bộ (loopback), và lỗi không chép địa chỉ (có thể mang mã truy cập).
func TestImageFetcher_BlocksInsecureAndInternal(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(jpegHead) }))
	defer srv.Close()
	var f nhanh.ImageFetcher
	if _, _, err := f.Fetch(context.Background(), "http://example.test/a.jpg"); !errors.Is(err, nhanh.ErrImageURL) {
		t.Errorf("http: %v", err)
	}
	_, _, err := f.Fetch(context.Background(), srv.URL+"/a.jpg?token=BIMAT")
	if err == nil {
		t.Fatal("loopback phải bị chặn")
	}
	if strings.Contains(err.Error(), "BIMAT") || strings.Contains(err.Error(), srv.URL) {
		t.Errorf("lỗi lộ địa chỉ: %v", err)
	}
}
