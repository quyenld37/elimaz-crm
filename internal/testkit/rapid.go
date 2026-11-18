package testkit

import (
	"flag"
	"os"
	"strconv"
)

// SetRapidChecks đặt số lượt mặc định của rapid (cờ -rapid.checks) thành n, trừ khi người chạy đã đặt -rapid.checks
// hay biến RAPID_CHECKS. Gọi trong TestMain sau flag.Parse (kế hoạch ghi số lượt cho từng quy luật).
func SetRapidChecks(n int) {
	explicit := os.Getenv("RAPID_CHECKS") != "" //nolint:forbidigo // testkit chỉ chạy trong test; biến của rapid
	flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "rapid.checks" })
	if !explicit {
		_ = flag.Set("rapid.checks", strconv.Itoa(n))
	}
}
