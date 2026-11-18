package xlsx_test

import (
	"flag"
	"os"
	"testing"

	"elimaz.com.vn/crm/internal/testkit"
)

// TestMain nâng số lượt rapid lên 500 (kế hoạch Task M1-38: thuộc tính của Sanitize 500 lượt), trừ khi người chạy đã
// đặt -rapid.checks hay RAPID_CHECKS.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(500)
	os.Exit(m.Run())
}
