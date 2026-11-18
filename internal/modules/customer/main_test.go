package customer_test

import (
	"flag"
	"os"
	"testing"

	"elimaz.com.vn/crm/internal/testkit"
)

// TestMain nâng số lượt rapid lên 200 (kế hoạch M2-11: quy luật gộp, hoàn tác 200 lượt), trừ khi người chạy đã đặt
// -rapid.checks hay RAPID_CHECKS.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(200)
	os.Exit(m.Run())
}
