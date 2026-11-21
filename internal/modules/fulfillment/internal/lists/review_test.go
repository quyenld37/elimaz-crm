package lists_test

import (
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/lists"
)

// Mọi loại anomaly mà CHECK của bảng cho phép (0300, 0312) có câu tiếng Việt riêng kèm hướng xử lý.
func TestLabel_EveryKind(t *testing.T) {
	kinds := []string{
		"in_doubt_create", "in_doubt_cancel", "manual_cancel", "invalid_edge", "unmapped_status",
		"tracking_conflict", "cancel_conflict", "partial_reported", "lost_or_damaged", "missing_items",
		"not_current_moved", "stock_short",
	}
	if !slices.Equal(lists.Kinds(), kinds) {
		t.Fatalf("Kinds = %v, muốn %v", lists.Kinds(), kinds)
	}
	seen := map[string]string{}
	for _, k := range kinds {
		l := lists.Label(k)
		if l == "" || l == lists.Label("zzz") || !strings.Contains(l, " — ") {
			t.Errorf("%s: nhãn %q thiếu câu riêng hay hướng xử lý", k, l)
		}
		if o, dup := seen[l]; dup {
			t.Errorf("%s trùng nhãn với %s", k, o)
		}
		seen[l] = k
	}
	if want := "Không rõ hãng đã tạo vận đơn chưa — kiểm trên trang hãng rồi bấm Đã tạo / Chưa tạo"; lists.Label("in_doubt_create") != want {
		t.Fatalf("in_doubt_create = %q", lists.Label("in_doubt_create"))
	}
}

// Loại lạ (task sau thêm vào CHECK) vẫn có câu chung, không rỗng. in_doubt_create (Đã tạo / Chưa tạo), manual_cancel,
// in_doubt_cancel (Ghi tay trạng thái, tự đóng việc) không đóng được ở đây; câu hướng dẫn chỉ tới đúng đường đó.
func TestLabel_UnknownAndResolvable(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		k := rapid.StringMatching(`[a-z_]{1,30}`).Draw(t, "kind")
		if lists.Label(k) == "" {
			t.Fatalf("nhãn rỗng cho %q", k)
		}
		if lists.Resolvable(k) != !slices.Contains([]string{"in_doubt_create", "manual_cancel", "in_doubt_cancel"}, k) {
			t.Fatalf("Resolvable(%q) = %v", k, lists.Resolvable(k))
		}
	})
	for _, k := range []string{"in_doubt_create", "manual_cancel", "in_doubt_cancel"} {
		if lists.Resolvable(k) {
			t.Errorf("%s đóng được ở đây", k)
		}
	}
	for _, k := range []string{"manual_cancel", "in_doubt_cancel"} {
		if !strings.Contains(lists.Label(k), "Ghi tay trạng thái") || strings.Contains(lists.Label(k), "đóng việc") {
			t.Errorf("%s: câu %q phải chỉ tới Ghi tay trạng thái, không bảo đóng việc", k, lists.Label(k))
		}
	}
	if !slices.Equal(lists.InDoubtKinds(), []string{"in_doubt_create", "in_doubt_cancel"}) {
		t.Fatalf("InDoubtKinds = %v", lists.InDoubtKinds())
	}
}
