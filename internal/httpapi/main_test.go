package httpapi

import (
	"flag"
	"os"
	"testing"

	"elimaz.com.vn/crm/internal/testkit"
)

// httpapiParallel là số test chạy song song mặc định của gói (Task 474, như orderParallel của Task 469): mỗi test có
// database riêng và tới ~9 kết nối PostgreSQL; máy chủ thử dùng chung nhiều phiên có max_connections 300.
const httpapiParallel = 6

// TestMain đặt -parallel thành min(httpapiParallel, GOMAXPROCS), trừ khi người chạy đã đặt -parallel.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetParallel(httpapiParallel)
	os.Exit(m.Run())
}
