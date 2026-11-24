package arch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// writeTree ghi các tệp (đường dẫn dấu "/" → nội dung) vào một thư mục tạm và trả thư mục đó.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func goFileRels(files []goFile) []string {
	out := make([]string, 0, len(files))
	for _, g := range files {
		out = append(out, g.rel)
	}
	slices.Sort(out)
	return out
}

// Bộ đệm cây đã phân tích (Task 480): mỗi cây phân tích một lần cho mọi test trong tiến trình; mỗi lần gọi vẫn lọc
// theo keep của bên gọi, và tệp hỏng chỉ làm lỗi khi bên gọi chọn nó — y như khi chưa có bộ đệm.
func TestParseTreeCachedAndFiltered(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, map[string]string{
		"a/a.go":                 "package a\n\nfunc A() {}\n",
		"a/a_test.go":            "package a\n",
		"b/testdata/broken.go":   "package broken\n\nfunc {\n",
		"node_modules/x/x.go":    "package x\n",
		"c/notes.txt":            "không phải Go",
		".worktrees/w/hidden.go": "package hidden\n",
	})
	noBroken := func(rel string) bool { return !isTestdata(rel) }
	first, err := parseTree(dir, noBroken)
	if err != nil {
		t.Fatalf("cây có tệp hỏng không được chọn: %v", err)
	}
	if got, want := goFileRels(first), []string{"a/a.go", "a/a_test.go"}; !slices.Equal(got, want) {
		t.Fatalf("tệp quét = %v, muốn %v", got, want)
	}
	onlyTests, err := parseTree(dir, func(rel string) bool { return strings.HasSuffix(rel, "_test.go") })
	if err != nil {
		t.Fatal(err)
	}
	if got := goFileRels(onlyTests); !slices.Equal(got, []string{"a/a_test.go"}) {
		t.Fatalf("lọc theo keep của lần gọi sau = %v", got)
	}
	again, err := parseTree(dir, noBroken)
	if err != nil {
		t.Fatal(err)
	}
	for i := range first {
		if first[i].file != again[i].file {
			t.Fatalf("%s bị phân tích lại: bộ đệm không dùng", first[i].rel)
		}
	}
	if _, err := parseTree(dir, func(string) bool { return true }); err == nil ||
		!strings.Contains(err.Error(), "b/testdata/broken.go") {
		t.Fatalf("chọn tệp hỏng phải lỗi và nêu tên tệp, có %v", err)
	}
}

// Lọc rẻ trước regexp (Task 480): bỏ qua tệp chỉ khi chắc chắn regexp không khớp. Chữ của mẫu lọc không có dạng
// hoa/thường ngoài ASCII (như ſ của s, K của k) nên ToLower rồi tìm chuỗi không bỏ sót chỗ (?i) khớp.
func TestCustomerWritePrefilterNeverMissesRegexp(t *testing.T) {
	t.Parallel()
	for _, r := range customerPrefilter {
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if f > unicode.MaxASCII {
				t.Fatalf("chữ %q của mẫu lọc có dạng gập ngoài ASCII %q: ToLower có thể bỏ sót", r, f)
			}
		}
	}
	samples := []string{
		"UPDATE customer.customers SET x = 1",
		"update\n    CUSTOMER.Customers set x = 1",
		"INSERT  INTO Customer.CUSTOMERS (a) VALUES (1)",
		"delete from customer.customers where id = 1",
		"SELECT * FROM customer.customers_v",
		"customer.customers",
		"không liên quan",
		"",
	}
	for _, s := range samples {
		if writeCustomers.MatchString(s) && !mayWriteCustomers([]byte(s)) {
			t.Errorf("lọc rẻ bỏ sót câu regexp khớp: %q", s)
		}
	}
	if mayWriteCustomers([]byte("package x\n\nfunc F() {}\n")) {
		t.Error("tệp không nhắc customer.customers phải bị lọc bỏ")
	}
}

// TestNoErrorStringCompareInRealCode dùng bộ đệm chung (Task 480) với bộ lọc isProductGoFile thay cho vòng duyệt riêng:
// bỏ _test.go và thư mục testdata, spikes ở mọi tầng, như vòng duyệt cũ.
func TestIsProductGoFile(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"internal/modules/order/create.go":        true,
		"main.go":                                 true,
		"internal/modules/order/create_test.go":   false,
		"tests/arch/testdata/lint/bad.go":         false,
		"testdata/x.go":                           false,
		"spikes/flashsale/main.go":                false,
		"internal/x/spikes/y.go":                  false,
		"internal/testdatax/y.go":                 true,
		"internal/spikesx/y.go":                   true,
		"internal/modules/order/testdata_util.go": true,
	}
	for rel, want := range cases {
		if got := isProductGoFile(rel); got != want {
			t.Errorf("isProductGoFile(%q) = %v, muốn %v", rel, got, want)
		}
	}
}
