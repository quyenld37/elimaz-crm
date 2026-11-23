package testkit_test

import (
	"flag"
	"runtime"
	"strconv"
	"testing"

	"elimaz.com.vn/crm/internal/testkit"
)

// SetParallel đặt -test.parallel thành min(n, GOMAXPROCS) khi chưa ai đặt -parallel; đã đặt thì giữ nguyên.
// Test đổi cờ chung của tiến trình nên không gọi t.Parallel và trả lại giá trị cũ khi xong.
func TestSetParallel(t *testing.T) {
	f := flag.Lookup("test.parallel")
	if f == nil {
		t.Fatal("không có cờ test.parallel")
	}
	before := f.Value.String()
	t.Cleanup(func() { _ = flag.Set("test.parallel", before) })
	explicit := false
	flag.Visit(func(v *flag.Flag) { explicit = explicit || v.Name == "test.parallel" })

	procs := runtime.GOMAXPROCS(0)
	testkit.SetParallel(procs + 5)
	want := strconv.Itoa(procs)
	if explicit {
		want = before
	}
	if got := f.Value.String(); got != want {
		t.Errorf("SetParallel(GOMAXPROCS+5): test.parallel = %s, muốn %s (người chạy đặt -parallel: %v)", got, want, explicit)
	}
	// Giá trị đã đặt (bởi người chạy hay lần gọi trước) được giữ: lần gọi sau không đổi.
	testkit.SetParallel(1)
	if got := f.Value.String(); got != want {
		t.Errorf("gọi lần hai: test.parallel = %s, muốn giữ %s", got, want)
	}
}
