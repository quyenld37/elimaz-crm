package nhanh_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/nhanh"
)

// Tệp 0600 trong thư mục 0700, không để lại tệp tạm; ghi hai lần cùng giờ ghi đè nguyên vẹn.
func TestWriteSnapshot_PrivateAtomic(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "snap")
	s := &nhanh.Snapshot{Version: 1, TakenAt: time.Date(2026, 12, 1, 3, 4, 5, 0, time.UTC)}
	path, err := nhanh.WriteSnapshot(dir, s)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "nhanh-snapshot-20261201T030405Z.json" {
		t.Errorf("tên = %s", path)
	}
	fi, _ := os.Stat(path)
	di, _ := os.Stat(dir)
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Errorf("quyền tệp %v, thư mục %v", fi.Mode().Perm(), di.Mode().Perm())
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Errorf("thư mục có %d mục, muốn 1 (không tệp tạm)", len(ents))
	}
}

// Xóa tệp quá 30 ngày, giữ tệp mới, không đụng tệp lạ tên.
func TestPruneSnapshots_ThirtyDays(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	for _, n := range []string{
		"nhanh-snapshot-20261125T000000Z.json", // 36 ngày: xóa
		"nhanh-snapshot-20261201T000000Z.json", // đúng 30 ngày: giữ
		"nhanh-snapshot-20261220T000000Z.json", // giữ
		"ghi-chu.json", "nhanh-snapshot-khac.json",
	} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	n, err := nhanh.PruneSnapshots(dir, now)
	if err != nil || n != 1 {
		t.Fatalf("xóa %d, lỗi %v", n, err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 4 {
		t.Errorf("còn %d tệp, muốn 4", len(ents))
	}
	if n, err := nhanh.PruneSnapshots(filepath.Join(dir, "khong-co"), now); n != 0 || err != nil {
		t.Errorf("thư mục chưa có: %d, %v", n, err)
	}
}
