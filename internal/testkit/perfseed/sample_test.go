package perfseed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestSample_HasEveryKeyK6Reads: m2-sample.json là hợp đồng giữa seedperf và k6 (loadtest/k6/*.js đọc SAMPLE.<khóa>);
// mỗi khóa k6 đọc phải có trong JSON của Sample, đúng chữ hoa thường (thiếu khóa thì k6 ra undefined, SĐT thành NaN).
func TestSample_HasEveryKeyK6Reads(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(Sample{})
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "loadtest", "k6", "m2*.js"))
	if err != nil || len(files) == 0 {
		t.Fatalf("không thấy loadtest/k6/m2*.js: %v", err)
	}
	read := regexp.MustCompile(`SAMPLE\.([A-Za-z0-9_]+)`)
	seen := 0
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range read.FindAllStringSubmatch(string(src), -1) {
			seen++
			if _, ok := keys[m[1]]; !ok {
				t.Errorf("%s đọc SAMPLE.%s nhưng JSON của Sample không có khóa đó", filepath.Base(f), m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("k6 không đọc khóa SAMPLE nào: biểu thức dò sai")
	}
}
