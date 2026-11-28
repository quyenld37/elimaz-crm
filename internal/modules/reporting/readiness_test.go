package reporting_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
	"elimaz.com.vn/crm/internal/testkit/shoptest"
)

// readyEnv là một tenant thử của báo cáo sẵn sàng (Task 269): dựng dữ liệu bằng chủ bảng, đánh giá bằng crm_app.
type readyEnv struct {
	t      *testing.T
	s      *testkit.Staff
	tenant uuid.UUID
	seed   *reportseed.Seed
	shelf  shoptest.Shelf
	now    time.Time
	page   uuid.UUID // Page gần nhất tạo bằng addPage
}

func newReadyEnv(t *testing.T, s *testkit.Staff) *readyEnv {
	t.Helper()
	tenant := s.Tenant(t, "ready-"+uuid.NewString())
	e := &readyEnv{t: t, s: s, tenant: tenant, seed: reportseed.New(t, s.Owner, tenant), now: time.Now()}
	s.Exec(t, tenant, func(tx pgx.Tx) error { e.shelf = shoptest.SeedShelf(t, tx, tenant); return nil })
	return e
}

// exec chạy một câu ghi bằng chủ bảng trong ngữ cảnh tenant.
func (e *readyEnv) exec(sql string, args ...any) {
	e.t.Helper()
	run := func(tx pgx.Tx) error {
		if _, err := tx.Exec(e.t.Context(), sql, args...); err != nil {
			return fmt.Errorf("exec: %w", err)
		}
		return nil
	}
	e.s.Exec(e.t, e.tenant, run)
}

// eval chạy báo cáo bằng crm_app trong ngữ cảnh tenant.
func (e *readyEnv) eval() reporting.Readiness {
	e.t.Helper()
	var r reporting.Readiness
	if err := tenancy.WithTenant(e.t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		r, err = reporting.LaunchReadiness(e.t.Context(), tx, e.s.App, e.now)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("LaunchReadiness: %v", err)
	}
	return r
}

// item là mục key của kết quả (test đỏ khi thiếu).
func item(t *testing.T, r reporting.Readiness, key string) reporting.ReadinessItem {
	t.Helper()
	for _, it := range r.Items {
		if it.Key == key {
			return it
		}
	}
	t.Fatalf("thiếu mục %s", key)
	return reporting.ReadinessItem{}
}

func (e *readyEnv) draft(code string, lack shoptest.Lack) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.s.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		id, _ = shoptest.DraftProduct(e.t, tx, e.tenant, e.seed.Own(), e.shelf, code, lack)
		return nil
	})
	return id
}

func (e *readyEnv) addPage() uuid.UUID {
	e.t.Helper()
	e.page = e.seed.Page("Page-" + uuid.NewString()[:8])
	return e.page
}

func (e *readyEnv) rule(page uuid.UUID, kind string, enabled bool) {
	e.t.Helper()
	e.exec(`INSERT INTO conversation.automation_rules (tenant_id, channel_account_id, kind, config, enabled)
		VALUES ($1, $2, $3, '{}', $4)`, e.tenant, page, kind, enabled)
}

func (e *readyEnv) allRules(page uuid.UUID) {
	e.t.Helper()
	e.rule(page, "auto_reply_comment", true)
	e.rule(page, "greeting", true)
	e.rule(page, "auto_hide", false)
}

func (e *readyEnv) carrier(checked *time.Time) {
	e.t.Helper()
	wh := e.seed.Warehouse("Kho-" + uuid.NewString()[:6])
	e.exec(`INSERT INTO fulfillment.carrier_accounts (tenant_id, seller_id, carrier_key, name, external_account_ref, warehouse_id, mode, last_check_at)
		VALUES ($1, $2, 'ghn', 'GHN', $3, $4, 'api', $5)`, e.tenant, e.seed.Own(), uuid.NewString(), wh, checked)
}

func (e *readyEnv) coupon(code string, from, to time.Time, status string) {
	e.t.Helper()
	e.exec(`INSERT INTO promotion.coupons (tenant_id, code, name, discount_kind, value, valid_from, valid_to, status, created_by)
		VALUES ($1, $2, $7, 'fixed', 10000, $3, $4, $5, $6)`, e.tenant, code, from, to, status, e.seed.Staff("Người tạo"), "Mã "+code)
}

// step là một thay đổi dữ liệu và trạng thái mong đợi của mục ngay sau đó.
type step struct {
	name string
	do   func(e *readyEnv)
	want string
}

// runSteps chạy lần lượt các bước của mục key, sau mỗi bước đánh giá lại và so trạng thái.
func runSteps(t *testing.T, e *readyEnv, key string, steps []step) {
	t.Helper()
	for _, st := range steps {
		st.do(e)
		if got := item(t, e.eval(), key); got.Status != st.want {
			t.Fatalf("%s: %q: status %q (value %q), mong %q", key, st.name, got.Status, got.Value, st.want)
		}
	}
}

// TestLaunchReadiness_FailThenOK: mỗi mục lần lượt thiếu (fail hay warn) rồi sửa thành ok; các bước chạy nối tiếp trên
// một tenant. Mục hệ thống (sao lưu, cảnh báo) ở TestLaunchReadiness_Platform vì dùng bảng không theo tenant.
func TestLaunchReadiness_FailThenOK(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	cases := readyCasesCatalogInventory()
	maps.Copy(cases, readyCasesChannelConversation())
	maps.Copy(cases, readyCasesSalesShipping())
	for key, steps := range cases {
		t.Run(key, func(t *testing.T) {
			e := newReadyEnv(t, s)
			runSteps(t, e, key, steps)
		})
	}
}

// TestLaunchReadiness_Platform: sao lưu, diễn tập khôi phục, cảnh báo gấp (bảng nền tảng không theo tenant).
func TestLaunchReadiness_Platform(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	e := newReadyEnv(t, s)
	ctx := t.Context()
	run := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.Owner.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	backup := func(kind string, ok bool, ago string) {
		t.Helper()
		run(`INSERT INTO platform.backup_runs (kind, ok, finished_at) VALUES ($1, $2, now() - $3::interval)`, kind, ok, ago)
	}
	cases := map[string][]step{
		"platform.backup_fresh": {
			{"chưa có bản nào", func(*readyEnv) {}, reporting.ReadinessFail},
			{"bản 30 giờ trước", func(*readyEnv) { backup("base", true, "30 hours") }, reporting.ReadinessFail},
			{"bản 25 giờ trước", func(*readyEnv) { backup("base", true, "25 hours") }, reporting.ReadinessOK},
			{"bản 26 giờ trước", func(*readyEnv) { run(`DELETE FROM platform.backup_runs`); backup("base", true, "26 hours 5 minutes") }, reporting.ReadinessFail},
			{"bản mới", func(*readyEnv) { backup("base", true, "1 hour") }, reporting.ReadinessOK},
		},
		"platform.restore_drill_ok": {
			{"chưa diễn tập", func(*readyEnv) { run(`DELETE FROM platform.backup_runs`) }, reporting.ReadinessFail},
			{"diễn tập hỏng", func(*readyEnv) { backup("restore_drill", false, "2 hours") }, reporting.ReadinessFail},
			{"diễn tập đạt gần nhất", func(*readyEnv) { backup("restore_drill", true, "1 hour") }, reporting.ReadinessOK},
			{"lần gần nhất hỏng lại", func(*readyEnv) { backup("restore_drill", false, "1 minute") }, reporting.ReadinessFail},
		},
		"platform.urgent_open": {
			{"không cảnh báo", func(*readyEnv) {}, reporting.ReadinessOK},
			{"chỉ cảnh báo thường", func(*readyEnv) {
				run(`INSERT INTO platform.alert_events (key, severity, text) VALUES ('queue_old', 'normal', 'x')`)
			}, reporting.ReadinessOK},
			{"cảnh báo gấp", func(*readyEnv) {
				run(`INSERT INTO platform.alert_events (key, severity, text) VALUES ('backup_stale', 'urgent', 'x')`)
			}, reporting.ReadinessFail},
			{"cảnh báo gấp quá 24 giờ", func(*readyEnv) {
				run(`UPDATE platform.alert_events SET sent_at = now() - interval '30 hours'`)
			}, reporting.ReadinessOK},
		},
	}
	for key, steps := range cases {
		runSteps(t, e, key, steps)
	}
}

// TestLaunchReadiness_EmptyShop: shop rỗng — thứ tự, trạng thái, đếm fail/warn, bốn mục chưa có nguồn.
func TestLaunchReadiness_EmptyShop(t *testing.T) {
	t.Parallel()
	e := newReadyEnv(t, testkit.OpenStaff(t))
	r := e.eval()
	want := []struct{ key, status string }{
		{"catalog.ready_ratio", reporting.ReadinessWarn},
		{"catalog.images_processing", reporting.ReadinessOK},
		{"inventory.warehouses_active", reporting.ReadinessFail},
		{"identity.enrollment_complete", reporting.ReadinessWarn},
		{"identity.no_grant_users", reporting.ReadinessWarn},
		{"channel.pages_connected", reporting.ReadinessWarn},
		{"conversation.rules_prepared", reporting.ReadinessWarn},
		{"conversation.templates_tags", reporting.ReadinessFail},
		{"order.traffic_sources", reporting.ReadinessFail},
		{"fulfillment.carriers_tested", reporting.ReadinessOK},
		{"promotion.coupons_active", reporting.ReadinessWarn},
		{"platform.backup_fresh", reporting.ReadinessFail},
		{"platform.restore_drill_ok", reporting.ReadinessFail},
		{"platform.urgent_open", reporting.ReadinessOK},
		{"conversation.t2_pass", reporting.ReadinessWarn},
	}
	if len(r.Items) != len(want) {
		t.Fatalf("%d mục, mong %d: %+v", len(r.Items), len(want), r.Items)
	}
	for i, w := range want {
		if r.Items[i].Key != w.key || r.Items[i].Status != w.status {
			t.Errorf("mục %d: %s %s, mong %s %s", i, r.Items[i].Key, r.Items[i].Status, w.key, w.status)
		}
		if r.Items[i].Group == "" || r.Items[i].Target == "" || r.Items[i].Value == "" {
			t.Errorf("mục %s thiếu nhóm, giá trị hay ngưỡng: %+v", w.key, r.Items[i])
		}
	}
	if r.Fail != 5 || r.Warn != 7 {
		t.Errorf("fail %d warn %d, mong 5 và 7", r.Fail, r.Warn)
	}
	for _, key := range []string{"identity.enrollment_complete", "identity.no_grant_users", "conversation.t2_pass"} {
		it := item(t, r, key)
		if it.Status == reporting.ReadinessOK || it.Value != "chưa có nguồn" || !strings.Contains(it.Target, "để sau") {
			t.Errorf("%s phải là warn \"chưa có nguồn\" kèm task để sau: %+v", key, it)
		}
	}
}

// TestLaunchReadiness_NoPersonalData: tên Page, người, sản phẩm gieo vào dữ liệu thử không lọt vào kết quả.
func TestLaunchReadiness_NoPersonalData(t *testing.T) {
	t.Parallel()
	e := newReadyEnv(t, testkit.OpenStaff(t))
	e.seed.Page("BÍ-MẬT-PAGE-Nguyễn Văn A")
	e.seed.Staff("BÍ-MẬT-NHÂN-VIÊN")
	e.seed.Source("BÍ-MẬT-NGUỒN")
	e.draft("BM1", shoptest.Lack{NoImage: true})
	e.exec(`UPDATE catalog.products SET name = 'BÍ-MẬT-SẢN-PHẨM'`)
	b, err := json.Marshal(e.eval())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"BÍ-MẬT", "@", "Nguyễn"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("kết quả chứa %q: %s", bad, b)
		}
	}
}

func readyCasesCatalogInventory() map[string][]step {
	return map[string][]step{
		"catalog.ready_ratio": {
			{"không có mẫu nháp", func(*readyEnv) {}, reporting.ReadinessWarn},
			{"một đủ một thiếu ảnh (1/2)", func(e *readyEnv) { e.draft("OK0", shoptest.Lack{}); e.draft("BAD", shoptest.Lack{NoImage: true}) }, reporting.ReadinessFail},
			{"19/20 (đúng 95%)", func(e *readyEnv) {
				for i := range 18 {
					e.draft("OK"+string(rune('A'+i)), shoptest.Lack{})
				}
			}, reporting.ReadinessWarn},
			{"đủ cả 20", func(e *readyEnv) {
				e.exec(`UPDATE catalog.products SET status = 'active' WHERE code = 'BAD'`)
			}, reporting.ReadinessOK},
		},
		"catalog.images_processing": {
			{"ảnh vừa tải lên (chưa quá 30 phút)", func(e *readyEnv) { e.draft("IMG", shoptest.Lack{ProcessingImage: true}) }, reporting.ReadinessOK},
			{"ảnh kẹt 1 giờ", func(e *readyEnv) {
				e.exec(`UPDATE platform.files SET updated_at = $1 WHERE status = 'uploaded'`, e.now.Add(-time.Hour))
			}, reporting.ReadinessFail},
			{"ảnh xử lý xong", func(e *readyEnv) { e.exec(`UPDATE platform.files SET status = 'ready' WHERE status = 'uploaded'`) }, reporting.ReadinessOK},
		},
		"inventory.warehouses_active": {
			{"chưa có kho", func(*readyEnv) {}, reporting.ReadinessFail},
			{"một kho active chưa mở sổ", func(e *readyEnv) { e.seed.Warehouse("Kho A") }, reporting.ReadinessOK},
			{"kho đã có dòng sổ", func(e *readyEnv) {
				e.seed.Ledger(e.seed.Variant(e.seed.Own(), "Áo", "AO-1"), e.seed.Warehouse("Kho B"), "on_hand", 5, "import", e.now)
			}, reporting.ReadinessFail},
		},
	}
}

func readyCasesChannelConversation() map[string][]step {
	return map[string][]step{
		"channel.pages_connected": {
			{"chưa nối Page", func(*readyEnv) {}, reporting.ReadinessWarn},
			{"Page chưa kiểm token", func(e *readyEnv) { e.addPage() }, reporting.ReadinessFail},
			{"token kiểm hôm nay, chỉ đọc", func(e *readyEnv) {
				e.exec(`UPDATE channel.channel_accounts SET token_checked_at = $1`, e.now)
			}, reporting.ReadinessOK},
			{"token kiểm 25 giờ trước", func(e *readyEnv) {
				e.exec(`UPDATE channel.channel_accounts SET token_checked_at = $1`, e.now.Add(-25*time.Hour))
			}, reporting.ReadinessFail},
			{"kiểm lại nhưng đã mở gửi", func(e *readyEnv) {
				e.exec(`UPDATE channel.channel_accounts SET token_checked_at = $1, send_mode = 'enabled'`, e.now)
			}, reporting.ReadinessFail},
			{"về chỉ đọc nhưng Page bị ngắt", func(e *readyEnv) {
				e.exec(`UPDATE channel.channel_accounts SET send_mode = 'read_only', status = 'disconnected'`)
			}, reporting.ReadinessFail},
			{"Page hoạt động lại", func(e *readyEnv) { e.exec(`UPDATE channel.channel_accounts SET status = 'active'`) }, reporting.ReadinessOK},
		},
		"conversation.rules_prepared": {
			{"chưa nối Page", func(*readyEnv) {}, reporting.ReadinessWarn},
			{"Page chưa có cấu hình", func(e *readyEnv) { e.addPage() }, reporting.ReadinessFail},
			{"đủ ba cấu hình, ẩn bình luận đang tắt", func(e *readyEnv) { e.allRules(e.page) }, reporting.ReadinessOK},
			{"ẩn bình luận đang bật", func(e *readyEnv) {
				e.exec(`UPDATE conversation.automation_rules SET enabled = true WHERE kind = 'auto_hide'`)
			}, reporting.ReadinessFail},
			{"tắt lại", func(e *readyEnv) {
				e.exec(`UPDATE conversation.automation_rules SET enabled = false WHERE kind = 'auto_hide'`)
			}, reporting.ReadinessOK},
			{"thêm Page thứ hai chưa cấu hình", func(e *readyEnv) { e.addPage() }, reporting.ReadinessFail},
			{"Page thứ hai đủ cấu hình", func(e *readyEnv) { e.allRules(e.page) }, reporting.ReadinessOK},
		},
		"conversation.templates_tags": {
			{"chưa có gì", func(*readyEnv) {}, reporting.ReadinessFail},
			{"chỉ có mẫu trả lời", func(e *readyEnv) {
				e.exec(`INSERT INTO conversation.reply_templates (tenant_id, shortcut, title, body) VALUES ($1, 'chao', 'Chào', 'Xin chào')`, e.tenant)
			}, reporting.ReadinessFail},
			{"có cả nhãn", func(e *readyEnv) {
				e.exec(`INSERT INTO conversation.tags (tenant_id, name, color) VALUES ($1, 'Khách quen', 'green')`, e.tenant)
			}, reporting.ReadinessOK},
			{"nhãn đã xóa", func(e *readyEnv) { e.exec(`UPDATE conversation.tags SET deleted_at = now()`) }, reporting.ReadinessFail},
		},
	}
}

func readyCasesSalesShipping() map[string][]step {
	return map[string][]step{
		"order.traffic_sources": {
			{"chưa có nguồn", func(*readyEnv) {}, reporting.ReadinessFail},
			{"có nguồn, thiếu fallback", func(e *readyEnv) { e.seed.Source("Facebook") }, reporting.ReadinessWarn},
			{"fallback đang tắt", func(e *readyEnv) {
				e.exec(`INSERT INTO ord.traffic_sources (tenant_id, key, name, position, active) VALUES ($1, 'fallback', 'Khác', 99, false)`, e.tenant)
			}, reporting.ReadinessWarn},
			{"fallback bật", func(e *readyEnv) { e.exec(`UPDATE ord.traffic_sources SET active = true WHERE key = 'fallback'`) }, reporting.ReadinessOK},
			{"mọi nguồn tắt", func(e *readyEnv) { e.exec(`UPDATE ord.traffic_sources SET active = false`) }, reporting.ReadinessFail},
		},
		"fulfillment.carriers_tested": {
			{"không tài khoản api", func(*readyEnv) {}, reporting.ReadinessOK},
			{"tài khoản api chưa kiểm", func(e *readyEnv) { e.carrier(nil) }, reporting.ReadinessFail},
			{"kiểm hôm nay", func(e *readyEnv) { e.exec(`UPDATE fulfillment.carrier_accounts SET last_check_at = $1`, e.now) }, reporting.ReadinessOK},
			{"kiểm 30 giờ trước", func(e *readyEnv) {
				e.exec(`UPDATE fulfillment.carrier_accounts SET last_check_at = $1`, e.now.Add(-30*time.Hour))
			}, reporting.ReadinessFail},
			{"tài khoản thủ công không tính", func(e *readyEnv) { e.exec(`UPDATE fulfillment.carrier_accounts SET mode = 'manual'`) }, reporting.ReadinessOK},
		},
		"promotion.coupons_active": {
			{"không có mã", func(*readyEnv) {}, reporting.ReadinessWarn},
			{"mã đã hết hạn", func(e *readyEnv) {
				e.coupon("HETHAN", e.now.Add(-48*time.Hour), e.now.Add(-time.Hour), "active")
			}, reporting.ReadinessWarn},
			{"mã đang chạy", func(e *readyEnv) { e.coupon("CHAY", e.now.Add(-time.Hour), e.now.Add(48*time.Hour), "active") }, reporting.ReadinessOK},
			{"mã bị tắt", func(e *readyEnv) { e.exec(`UPDATE promotion.coupons SET status = 'disabled'`) }, reporting.ReadinessWarn},
		},
	}
}
