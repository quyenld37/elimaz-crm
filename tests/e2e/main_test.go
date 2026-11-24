package e2e

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"elimaz.com.vn/crm/internal/testkit"
)

// e2eParallel: số test chạy song song mặc định (Task 484). Mỗi test có database riêng và tiến trình con mở pool riêng;
// crm-shared có 300 kết nối dùng chung nhiều phiên, nên giới hạn như các gói lớn (Task 469, 470).
const e2eParallel = 6

// TestMain giới hạn số test song song và tạo thư mục chung cho tệp chạy dựng một lần (buildPackage); xóa khi xong.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetParallel(e2eParallel)
	dir, err := os.MkdirTemp("", "crm-e2e-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: tạo thư mục tệp chạy:", err)
		os.Exit(1)
	}
	builds.dir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
