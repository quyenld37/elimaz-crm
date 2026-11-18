package identity_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/modules/identity"
)

type quyenRow struct {
	scope string
	roles []string
}

// readQuyenDoc đọc các dòng bảng của docs/quyen.md (dòng bắt đầu bằng "| `"): khóa → loại phạm vi, vai trò mẫu.
func readQuyenDoc(t *testing.T) map[identity.Permission]quyenRow {
	t.Helper()
	raw, err := os.ReadFile("../../../docs/quyen.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := map[identity.Permission]quyenRow{}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 5 {
			t.Fatalf("dòng không đủ 5 cột: %s", line)
		}
		key := identity.Permission(strings.Trim(strings.TrimSpace(cells[0]), "`"))
		if _, dup := doc[key]; dup {
			t.Errorf("quyền %s ghi hai lần", key)
		}
		var roles []string
		for r := range strings.SplitSeq(cells[3], ",") {
			if r = strings.Trim(strings.TrimSpace(r), "`"); r != "" {
				roles = append(roles, r)
			}
		}
		slices.Sort(roles)
		doc[key] = quyenRow{scope: strings.TrimSpace(cells[1]), roles: roles}
	}
	return doc
}

// rolesWith: khóa (đã sắp) của các vai trò mẫu có quyền p.
func rolesWith(p identity.Permission) []string {
	var out []string
	for _, role := range identity.DefaultRoles {
		if slices.Contains(role.Permissions, p) {
			out = append(out, role.Key)
		}
	}
	slices.Sort(out)
	return out
}

// docs/quyen.md là bảng quyền chuẩn (kế hoạch M1, A9): tập khóa bằng đúng identity.Permissions, cùng loại phạm vi,
// và cột "vai trò mẫu nhận" bằng đúng các vai trò của DefaultRoles có quyền đó.
func TestPermissionsDoc_MatchesCode(t *testing.T) {
	doc := readQuyenDoc(t)
	scopeName := map[identity.ScopeKind]string{
		identity.ScopeNone: "none", identity.ScopeByTarget: "theo đích", identity.ScopeShop: "toàn shop",
	}
	for _, d := range identity.Permissions {
		r, ok := doc[d.Key]
		if !ok {
			t.Errorf("docs/quyen.md thiếu %s", d.Key)
			continue
		}
		if !strings.HasPrefix(r.scope, scopeName[d.Scope]) {
			t.Errorf("%s: loại phạm vi trong tài liệu %q, mã là %q", d.Key, r.scope, scopeName[d.Scope])
		}
		if want := rolesWith(d.Key); !slices.Equal(r.roles, want) {
			t.Errorf("%s: vai trò mẫu trong tài liệu %v, mã là %v", d.Key, r.roles, want)
		}
		delete(doc, d.Key)
	}
	for k := range doc {
		t.Errorf("docs/quyen.md có quyền %s không có trong identity.Permissions", k)
	}
}
