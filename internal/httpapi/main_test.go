package httpapi

import (
	"flag"
	"os"
	"runtime"
	"strconv"
	"testing"
)

// httpapiParallel là số test chạy song song mặc định của gói (Task 474, như orderParallel của Task 469): mỗi test có
// database riêng và tới ~9 kết nối PostgreSQL; máy chủ thử dùng chung nhiều phiên có max_connections 300.
const httpapiParallel = 6

// TestMain đặt -parallel thành min(httpapiParallel, GOMAXPROCS), trừ khi người chạy đã đặt -parallel.
func TestMain(m *testing.M) {
	flag.Parse()
	set := false
	flag.Visit(func(f *flag.Flag) { set = set || f.Name == "test.parallel" })
	if !set {
		_ = flag.Set("test.parallel", strconv.Itoa(min(httpapiParallel, runtime.GOMAXPROCS(0))))
	}
	os.Exit(m.Run())
}
