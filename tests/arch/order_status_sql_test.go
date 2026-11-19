package arch

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// transitionSQL là tệp duy nhất được chứa câu đổi trạng thái đơn (kế hoạch M2, Task M2-16): đổi trạng thái ở chỗ khác
// bỏ qua máy trạng thái, lịch sử và sự kiện order.*.
const transitionSQL = "ord/transition.sql"

var (
	updateOrdersRe = regexp.MustCompile(`(?is)\bUPDATE\s+ord\.orders\b(?:\s+(?:AS\s+)?[a-z_]\w*)?\s+SET\b(.*?)(?:\bWHERE\b|\bFROM\b|\bRETURNING\b|$)`)
	setStatusRe    = regexp.MustCompile(`(?i)(?:^|[\s,(])status\s*=`)
)

// orderStatusUpdates trả các tệp .sql dưới dir (đường dẫn tương đối) có câu UPDATE ord.orders gán cột status, trừ
// transition.sql.
func orderStatusUpdates(t *testing.T, dir string) []string {
	t.Helper()
	var bad []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".sql") {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		rel = filepath.ToSlash(rel)
		if rel == transitionSQL {
			return nil
		}
		b, err := os.ReadFile(path) //nolint:gosec // tệp trong repo
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		sql := strLitRe.ReplaceAllString(lineCmtRe.ReplaceAllString(string(b), " "), "''")
		for _, stmt := range strings.Split(sql, ";") {
			for _, m := range updateOrdersRe.FindAllStringSubmatch(stmt, -1) {
				if setStatusRe.MatchString(m[1]) {
					bad = append(bad, rel)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return bad
}

// TestOrderStatusSQL_OnlyInTransition: không tệp .sql nào ngoài queries/ord/transition.sql gán status của ord.orders.
func TestOrderStatusSQL_OnlyInTransition(t *testing.T) {
	if bad := orderStatusUpdates(t, filepath.Join(repoRoot(t), "queries")); len(bad) != 0 {
		t.Errorf("đổi trạng thái đơn ngoài %s: %v (dùng order.applyTransition)", transitionSQL, bad)
	}
}

// TestOrderStatusSQL_DetectsSample: mẫu sai (UPDATE ord.orders SET status ngoài transition.sql) bị báo; mẫu chỉ đọc
// status trong WHERE hay gán packing_status thì không.
func TestOrderStatusSQL_DetectsSample(t *testing.T) {
	got := orderStatusUpdates(t, filepath.Join("testdata", "orderstatus", "queries"))
	if len(got) != 1 || got[0] != "fulfillment/bad.sql" {
		t.Errorf("phát hiện %v, mong đúng [fulfillment/bad.sql]", got)
	}
}
