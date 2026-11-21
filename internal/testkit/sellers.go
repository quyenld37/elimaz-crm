package testkit

import (
	"strconv"
	"testing"

	"github.com/google/uuid"
)

// ForEachSeller chạy fn như một test con cho từng người bán trong sellers, lần lượt (không song song: các lần chạy
// thường dùng chung một database và tenant), tên test con "seller-<thứ tự>". Mỗi ca nghiệp vụ chạy với từng người bán
// phải đạt như nhau (KH14).
func ForEachSeller(t *testing.T, sellers []uuid.UUID, fn func(t *testing.T, sellerID uuid.UUID)) {
	t.Helper()
	if len(sellers) == 0 {
		t.Fatal("testkit: ForEachSeller cần ít nhất một người bán")
	}
	for i, s := range sellers {
		t.Run("seller-"+strconv.Itoa(i), func(t *testing.T) { fn(t, s) })
	}
}
