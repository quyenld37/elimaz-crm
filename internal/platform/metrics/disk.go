//go:build linux || darwin

package metrics

import (
	"fmt"
	"syscall"
)

// DiskUsedPct trả phần trăm đã dùng của hệ thống tệp chứa path, theo cách tính của df: đã dùng / (đã dùng + còn
// trống cho người dùng thường).
func DiskUsedPct(path string) (float64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("đo đĩa: %w", err)
	}
	used := st.Blocks - st.Bfree
	total := used + st.Bavail
	if total == 0 {
		return 0, nil
	}
	return float64(used) * 100 / float64(total), nil
}
