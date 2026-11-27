package reporting_test

import (
	"flag"
	"os"
	"testing"

	"elimaz.com.vn/crm/internal/testkit"
)

// TestMain đặt số lượt rapid của gói: 200 lượt cho quy luật phạm vi (kế hoạch M6, Task 227).
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(200)
	os.Exit(m.Run())
}
