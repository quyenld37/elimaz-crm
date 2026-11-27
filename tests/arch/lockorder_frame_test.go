package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Khung thứ tự khóa của M3, M4 (Task M2-33, Q-X4, R4) trong docs/adr/0046-thu-tu-khoa.md. Tệp tên riêng: M4-03 tạo
// lockorder_test.go (bảng trong migrations/04*.sql phải có tên trong ADR-46).

var (
	lockRowRe   = regexp.MustCompile(`^\| (\d+[a-z]?) \|`)
	lockTableRe = regexp.MustCompile("`([a-z_]+\\.[a-z_]+)`")
)

// lockOrderM0M2 là các dòng của M0–M2 lúc Task M2-33 viết khung (nhóm → bảng theo thứ tự). Mốc sau chỉ được chèn:
// mỗi danh sách phải còn nguyên, đúng thứ tự, trong đúng nhóm.
//
//nolint:gochecknoglobals // bảng mẫu chỉ đọc của test
var lockOrderM0M2 = map[string][]string{
	"1": {"platform.tenants", "platform.tenant_routes"},
	"2": {
		"identity.users", "identity.roles", "identity.role_permissions", "identity.user_role_grants",
		"identity.user_credentials", "identity.user_second_factors", "identity.user_sessions",
	},
	"3": {"channel.channel_accounts"},
	"4": {
		"catalog.imports", "catalog.sellers", "catalog.categories", "catalog.attributes", "catalog.attribute_values",
		"catalog.tags", "catalog.products", "catalog.product_attribute_values", "catalog.product_tags",
		"catalog.product_images", "catalog.variants", "catalog.barcode_counters",
	},
	"5": {
		"customer.customers", "customer.customer_identities", "customer.customer_addresses",
		"customer.customer_tag_links", "customer.customer_tag_rules", "customer.customer_merges", "customer.customer_imports",
	},
	"6": {"conversation.conversations", "conversation.live_sessions", "conversation.messages", "conversation.automation_runs"},
	"7": {
		"ord.orders", "ord.order_items", "ord.order_duplicate_links", "ord.order_status_history", "ord.returns",
		"ord.return_items", "ord.traffic_sources", "ord.order_settings",
	},
	"8": {"fulfillment.carrier_accounts", "fulfillment.shipments", "fulfillment.shipment_events"},
	"9": {"cash.cod_statements", "cash.cod_transfers", "cash.cod_ledger_entries", "cash.payments"},
	"10": {
		"promotion.coupon_batches", "promotion.coupons", "promotion.coupon_customer_uses",
		"promotion.coupon_redemptions", "promotion.flash_sales", "promotion.flash_sale_items", "promotion.flash_sale_claims",
	},
	"11": {
		"inventory.warehouses", "inventory.suppliers", "inventory.stock_documents", "inventory.stock_document_lines",
		"inventory.stocktakes", "inventory.stocktake_lines", "inventory.stock_balances", "inventory.stock_reservations",
		"inventory.flash_sale_units", "inventory.reconcile_runs",
	},
	"12":  {"affiliate.affiliates", "affiliate.commissions", "affiliate.payouts", "affiliate.payout_items"},
	"12a": {},
	"12b": {"platform.files", "platform.file_variants"},
	"13":  {"platform.idempotency_keys", "platform.jobs"},
}

// lockOrderFrame là khung của M3 (Global Constraints "Thứ tự khóa"), M4 (Task M4-03) và M6 (Task 225, Q19: nhóm 12a
// `reporting`): nhóm → bảng theo thứ tự (gồm cả bảng của M0–M2 trong nhóm đó).
//
//nolint:gochecknoglobals // bảng mẫu chỉ đọc của test
var lockOrderFrame = map[string][]string{
	"3": {"channel.channel_accounts", "channel.send_buckets", "channel.oauth_sessions"},
	"6": {
		"conversation.tags", "conversation.reply_templates", "conversation.page_settings", "conversation.posts",
		"conversation.conversations", "conversation.conversation_tag_links", "conversation.conversation_assignments",
		"conversation.reminders", "conversation.live_sessions", "conversation.live_comment_claims", "conversation.messages",
		"conversation.message_revisions", "conversation.automation_rules", "conversation.automation_runs",
		"conversation.call_logs", "conversation.presence",
	},
	"8": {
		"fulfillment.carrier_accounts", "fulfillment.shipments", "fulfillment.shipment_events",
		"fulfillment.callback_tasks", "fulfillment.address_change_requests", "fulfillment.shipment_anomalies",
		"fulfillment.label_batches", "fulfillment.shipping_quotes", "fulfillment.carrier_locations",
	},
	"9": {
		"cash.cod_statements", "cash.cod_statement_lines", "cash.cod_transfers", "cash.cod_ledger_entries",
		"cash.payments", "cash.refund_obligations", "cash.reconcile_runs",
	},
	"12a": {"reporting.report_runs", "reporting.stock_snapshot_days", "reporting.stock_daily_snapshots"},
	"12c": {"platform.settings"},
	"13":  {"platform.data_deletion_requests", "platform.idempotency_keys", "platform.jobs"},
}

// lockGroups đọc bảng thứ tự khóa của ADR-46: mã nhóm theo thứ tự dòng, và nhóm → bảng theo thứ tự trong ô.
func lockGroups(t *testing.T) (codes []string, tables map[string][]string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "adr", "0046-thu-tu-khoa.md"))
	if err != nil {
		t.Fatalf("đọc ADR-46: %v", err)
	}
	tables = map[string][]string{}
	for line := range strings.SplitSeq(string(b), "\n") {
		m := lockRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 5 {
			t.Fatalf("dòng ADR-46 sai dạng: %s", line)
		}
		codes = append(codes, m[1])
		for _, tm := range lockTableRe.FindAllStringSubmatch(cells[3], -1) {
			tables[m[1]] = append(tables[m[1]], tm[1])
		}
	}
	return codes, tables
}

// isSubsequence: want nằm trong got đúng thứ tự (được chèn bảng khác vào giữa).
func isSubsequence(want, got []string) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want)
}

// Mỗi mã nhóm đúng một dòng, theo thứ tự số rồi chữ (… 12 < 12a < 12b < 12c < 12d < 13; 12d content, Task 453).
func TestLockOrderGroupCodes(t *testing.T) {
	codes, _ := lockGroups(t)
	want := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "12a", "12b", "12c", "12d", "13"}
	if !slices.Equal(codes, want) {
		t.Fatalf("mã nhóm trong ADR-46 = %v, muốn đúng một lần mỗi mã theo thứ tự %v", codes, want)
	}
}

// Dòng của M0–M2 còn nguyên (chỉ được chèn); mọi bảng của khung M3, M4 có mặt đúng nhóm, đúng thứ tự; không bảng nào
// xuất hiện hai lần trong cả bảng thứ tự.
func TestLockOrderFrame(t *testing.T) {
	_, tables := lockGroups(t)
	for code, want := range lockOrderM0M2 {
		if !isSubsequence(want, tables[code]) {
			t.Errorf("nhóm %s: dòng của M0–M2 %v bị đổi (hiện là %v); chỉ được chèn", code, want, tables[code])
		}
	}
	for code, want := range lockOrderFrame {
		if !isSubsequence(want, tables[code]) {
			t.Errorf("nhóm %s: thiếu khung M3/M4 %v theo thứ tự (hiện là %v)", code, want, tables[code])
		}
	}
	seen := map[string]string{}
	for code, ts := range tables {
		for _, tb := range ts {
			if prev, dup := seen[tb]; dup {
				t.Errorf("bảng %s có ở nhóm %s và %s; mỗi bảng đúng một dòng", tb, prev, code)
			}
			seen[tb] = code
		}
	}
}
