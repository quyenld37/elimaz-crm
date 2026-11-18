package identity_test

import (
	"flag"
	"os"
	"testing"
)

// TestMain nâng số lượt rapid lên 200 (kế hoạch Task 7: quy luật của Can chạy 200 lượt), trừ khi người chạy đã đặt
// -rapid.checks hay RAPID_CHECKS.
func TestMain(m *testing.M) {
	flag.Parse()
	explicit := os.Getenv("RAPID_CHECKS") != ""
	flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "rapid.checks" })
	if !explicit {
		_ = flag.Set("rapid.checks", "200")
	}
	os.Exit(m.Run())
}
