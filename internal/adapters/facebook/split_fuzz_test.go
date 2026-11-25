package facebook_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/facebook"
)

var skippedLabel = regexp.MustCompile(`^[a-z0-9_]{1,40}(:[a-z0-9_]{1,40})?$`)

// FuzzSplit: thân bất kỳ không làm Split panic; tách được thì mọi sự kiện có loại, khóa chống trùng, khóa thứ tự,
// Page, giờ; nhãn Skipped chỉ là tên loại đã lọc (không bao giờ là nội dung); tách lại ra đúng cùng khóa.
// Chạy dài: go test -run '^$' -fuzz=FuzzSplit -fuzztime=30s ./internal/adapters/facebook/
func FuzzSplit(f *testing.F) {
	names, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil || len(names) == 0 {
		f.Fatalf("không có fixture: %v", err)
	}
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte(`{"object":"page","entry":[{"id":"1","messaging":[{}],"changes":[{}]}]}`))
	a := newAdapter()
	f.Fuzz(func(t *testing.T, raw []byte) { checkSplit(t, a, raw) })
}

// checkSplit kiểm các bất biến của Split trên một thân bất kỳ.
func checkSplit(t *testing.T, a *facebook.Channel, raw []byte) {
	t.Helper()
	res, err := a.Split(raw)
	if err != nil {
		return
	}
	for i, e := range res.Events {
		if e.Type == "" || e.DedupeKey == "" || e.OrderKey == "" || e.Route.ExternalKey == "" || e.OccurredAt.IsZero() ||
			len(e.Payload) == 0 {
			t.Fatalf("sự kiện %d thiếu trường: %+v", i, e)
		}
	}
	for k, n := range res.Skipped {
		if !skippedLabel.MatchString(k) || n <= 0 {
			t.Fatalf("nhãn Skipped %q = %d", k, n)
		}
	}
	again, err := a.Split(raw)
	if err != nil || !slices.Equal(got(again), got(res)) {
		t.Fatalf("tách lại khác: %v", err)
	}
}
