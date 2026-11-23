package testkit

import (
	"flag"
	"runtime"
	"strconv"
)

// SetParallel đặt số test chạy song song mặc định của gói (cờ -test.parallel) thành min(n, GOMAXPROCS), trừ khi người
// chạy đã đặt -parallel. Gọi trong TestMain sau flag.Parse (Task 469, 470: mỗi test có database riêng và tới 9 kết nối
// PostgreSQL; máy chủ thử dùng chung nhiều phiên có max_connections 300, nên gói lớn giới hạn số test cùng chạy).
func SetParallel(n int) {
	explicit := false
	flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "test.parallel" })
	if !explicit {
		_ = flag.Set("test.parallel", strconv.Itoa(min(n, runtime.GOMAXPROCS(0))))
	}
}
