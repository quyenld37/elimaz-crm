package reporting

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/reporting/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/observability"
)

// Báo cáo sẵn sàng dữ liệu cho buổi đi/dừng 24–48 giờ trước đêm mở (kế hoạch M7, Task 269 = M7-24). Chỉ đọc: view của
// module chủ (docs/views.md) trong transaction tenant của bên gọi, và observability.Summary cho sao lưu, diễn tập
// khôi phục, cảnh báo gấp. Kết quả chỉ có số đếm và mã, không tên người, tên khách, tên Page.

// Trạng thái của một mục.
const (
	ReadinessOK   = "ok"
	ReadinessWarn = "warn"
	ReadinessFail = "fail"
)

// Nhóm của mục (hiện trên màn hình).
const (
	groupProducts  = "Sản phẩm"
	groupInventory = "Kho"
	groupStaff     = "Nhân viên"
	groupChannel   = "Kênh"
	groupInbox     = "Hộp chat"
	groupOrders    = "Đơn"
	groupShipping  = "Vận chuyển"
	groupPromos    = "Khuyến mãi"
	groupSystem    = "Hệ thống"
	groupT2        = "T2"
)

// Ngưỡng của báo cáo.
const (
	// readinessReadyPct: tỉ lệ mẫu nháp đủ điều kiện tối thiểu (dưới mức này là fail; từ mức này tới dưới 100% là warn).
	readinessReadyPct = 95
	// readinessImageStuck: ảnh còn uploaded hay processing quá chừng này là kẹt.
	readinessImageStuck = 30 * time.Minute
	// readinessCheckFresh: mốc kiểm token Page, kiểm tài khoản hãng phải nằm trong chừng này.
	readinessCheckFresh = 24 * time.Hour
	// readinessBackupMaxHours: bản sao lưu đầy đủ phải dưới số giờ này tuổi.
	readinessBackupMaxHours = 26
)

// noSource là Value của mục mà task nguồn chưa làm (không giả ok).
const noSource = "chưa có nguồn"

// ReadinessItem là một mục kiểm. Status: ReadinessOK, ReadinessWarn hay ReadinessFail. Value là giá trị hiện tại,
// Target là ngưỡng đạt.
type ReadinessItem struct {
	Key, Group, Status, Value, Target string
}

// Readiness là kết quả đánh giá: các mục theo thứ tự cố định, số mục fail và warn.
type Readiness struct {
	Items       []ReadinessItem
	Fail, Warn  int
	EvaluatedAt time.Time
}

// LaunchReadiness đánh giá mọi mục sẵn sàng tại now. tx là transaction tenant (tenancy.WithTenant) cho view của
// module; d là pool ứng dụng cho dữ liệu nền tảng (không theo tenant). Chạy lại được, không ghi gì.
func LaunchReadiness(ctx context.Context, tx pgx.Tx, d *db.DB, now time.Time) (Readiness, error) {
	q := store.New(tx)
	cat, err := q.ReadinessCatalog(ctx, now.Add(-readinessImageStuck))
	if err != nil {
		return Readiness{}, fmt.Errorf("đọc sẵn sàng sản phẩm: %w", err)
	}
	wh, err := q.ReadinessWarehouses(ctx)
	if err != nil {
		return Readiness{}, fmt.Errorf("đọc sẵn sàng kho: %w", err)
	}
	fresh := now.Add(-readinessCheckFresh)
	pages, err := q.ReadinessPages(ctx, fresh)
	if err != nil {
		return Readiness{}, fmt.Errorf("đọc sẵn sàng Page: %w", err)
	}
	conv, err := q.ReadinessConversation(ctx)
	if err != nil {
		return Readiness{}, fmt.Errorf("đọc sẵn sàng hộp chat: %w", err)
	}
	src, err := q.ReadinessSources(ctx)
	if err != nil {
		return Readiness{}, fmt.Errorf("đọc sẵn sàng nguồn đơn: %w", err)
	}
	car, err := q.ReadinessCarriers(ctx, fresh)
	if err != nil {
		return Readiness{}, fmt.Errorf("đọc sẵn sàng hãng vận chuyển: %w", err)
	}
	cou, err := q.ReadinessCoupons(ctx, now)
	if err != nil {
		return Readiness{}, fmt.Errorf("đọc sẵn sàng mã giảm giá: %w", err)
	}
	health, err := observability.Summary(ctx, d, now)
	if err != nil {
		return Readiness{}, fmt.Errorf("đọc sức khỏe hệ thống: %w", err)
	}

	r := Readiness{EvaluatedAt: now, Items: []ReadinessItem{
		readyRatio(cat.DraftTotal, cat.DraftReady),
		imagesProcessing(cat.ImagesStuck),
		warehousesActive(wh.ActiveCount, wh.WithLedger),
		pending("identity.enrollment_complete", groupStaff, "100% nhân viên active có grant complete", "Task 253"),
		pending("identity.no_grant_users", groupStaff, "0 nhân viên active không có grant", "Task 253"),
		pagesConnected(pages.Total, pages.Connected),
		rulesPrepared(pages.Total, pages.Prepared),
		templatesTags(conv.Templates, conv.Tags),
		trafficSources(src.ActiveCount, src.FallbackCount, src.FallbackActive),
		carriersTested(car.Total, car.Tested),
		couponsActive(cou),
		backupFresh(health.Backup),
		restoreDrill(health.Backup),
		urgentOpen(health.Alerts),
		pending("conversation.t2_pass", groupT2, "Pass7Days của mọi Page", "Task 266"),
	}}
	for _, it := range r.Items {
		switch it.Status {
		case ReadinessFail:
			r.Fail++
		case ReadinessWarn:
			r.Warn++
		}
	}
	return r, nil
}

// pending là mục có nguồn thuộc task để sau: warn "chưa có nguồn", không bao giờ ok.
func pending(key, group, target, task string) ReadinessItem {
	return ReadinessItem{Key: key, Group: group, Status: ReadinessWarn, Value: noSource, Target: target + " (" + task + " để sau)"}
}

// ratio là "đạt/tổng".
func ratio(ok, total int64) string { return fmt.Sprintf("%d/%d", ok, total) }

// grade trả ok khi good, ngược lại bad (fail hay warn).
func grade(good bool, bad string) string {
	if !good {
		return bad
	}
	return ReadinessOK
}

func readyRatio(total, ready int64) ReadinessItem {
	it := ReadinessItem{
		Key: "catalog.ready_ratio", Group: groupProducts, Value: ratio(ready, total),
		Target: fmt.Sprintf("≥ %d%% mẫu nháp đủ điều kiện (cảnh báo dưới 100%%)", readinessReadyPct),
	}
	switch {
	case total == 0:
		it.Status = ReadinessWarn // không có mẫu nháp nào để mở bán
	case ready == total:
		it.Status = ReadinessOK
	case ready*100 >= readinessReadyPct*total:
		it.Status = ReadinessWarn
	default:
		it.Status = ReadinessFail
	}
	return it
}

func imagesProcessing(stuck int64) ReadinessItem {
	return ReadinessItem{
		Key: "catalog.images_processing", Group: groupProducts, Status: grade(stuck == 0, ReadinessFail),
		Value: fmt.Sprint(stuck), Target: "0 ảnh đang xử lý quá 30 phút",
	}
}

func warehousesActive(active, withLedger int64) ReadinessItem {
	return ReadinessItem{
		Key: "inventory.warehouses_active", Group: groupInventory, Status: grade(active >= 1 && withLedger == 0, ReadinessFail),
		Value:  fmt.Sprintf("%d kho active, %d đã có sổ tồn", active, withLedger),
		Target: "≥ 1 kho active, chưa kho nào có dòng sổ (chưa mở sổ)",
	}
}

func pagesConnected(total, connected int64) ReadinessItem {
	return perPage("channel.pages_connected", groupChannel, "mọi Page active, token kiểm trong 24 giờ, chế độ chỉ đọc", total, connected)
}

func rulesPrepared(total, prepared int64) ReadinessItem {
	return perPage("conversation.rules_prepared", groupInbox, "mỗi Page có cấu hình trả lời bình luận, chào, ẩn bình luận (đang tắt)", total, prepared)
}

// perPage điền Value "đạt/tổng" và Status của một mục đếm theo Page: chưa nối Page nào là warn, thiếu Page đạt là fail.
func perPage(key, group, target string, total, good int64) ReadinessItem {
	it := ReadinessItem{Key: key, Group: group, Target: target, Value: ratio(good, total)}
	if total == 0 {
		it.Status = ReadinessWarn
		return it
	}
	it.Status = grade(good == total, ReadinessFail)
	return it
}

func templatesTags(templates, tags int64) ReadinessItem {
	return ReadinessItem{
		Key: "conversation.templates_tags", Group: groupInbox, Status: grade(templates > 0 && tags > 0, ReadinessFail),
		Value: fmt.Sprintf("%d mẫu, %d nhãn", templates, tags), Target: "có mẫu trả lời và nhãn hội thoại",
	}
}

func trafficSources(active, fallback, fallbackActive int64) ReadinessItem {
	it := ReadinessItem{
		Key: "order.traffic_sources", Group: groupOrders, Value: fmt.Sprintf("%d nguồn active", active),
		Target: "≥ 1 nguồn active; nguồn fallback có và đang bật (nếu thiếu: cảnh báo)",
	}
	switch {
	case active == 0:
		it.Status = ReadinessFail
	case fallback == 0 || fallbackActive == 0:
		it.Status = ReadinessWarn // Task 273/M7-23 tự tạo khi thiếu; đang tắt thì lô nhập vẫn chạy và ghi cảnh báo
	default:
		it.Status = ReadinessOK
	}
	return it
}

func carriersTested(total, tested int64) ReadinessItem {
	return ReadinessItem{
		Key: "fulfillment.carriers_tested", Group: groupShipping, Status: grade(tested == total, ReadinessFail),
		Value: ratio(tested, total), Target: "mỗi tài khoản hãng api active, kiểm trong 24 giờ",
	}
}

func couponsActive(running int64) ReadinessItem {
	return ReadinessItem{
		Key: "promotion.coupons_active", Group: groupPromos, Status: grade(running > 0, ReadinessWarn),
		Value: fmt.Sprint(running), Target: "có mã đang chạy (cảnh báo khi 0)",
	}
}

func backupFresh(b observability.BackupHealth) ReadinessItem {
	it := ReadinessItem{
		Key: "platform.backup_fresh", Group: groupSystem, Value: "chưa có bản sao lưu",
		Target: fmt.Sprintf("bản đầy đủ dưới %d giờ", readinessBackupMaxHours),
	}
	if b.LastBaseAgeH != nil {
		it.Value = fmt.Sprintf("%d giờ trước", *b.LastBaseAgeH)
		it.Status = grade(*b.LastBaseAgeH < readinessBackupMaxHours, ReadinessFail)
		return it
	}
	it.Status = ReadinessFail
	return it
}

func restoreDrill(b observability.BackupHealth) ReadinessItem {
	it := ReadinessItem{
		Key: "platform.restore_drill_ok", Group: groupSystem, Status: ReadinessFail, Value: "chưa diễn tập",
		Target: "diễn tập khôi phục gần nhất đạt",
	}
	if b.LastDrillOk != nil && b.LastDrillAgeH != nil {
		it.Value = fmt.Sprintf("%s, %d giờ trước", map[bool]string{true: "đạt", false: "hỏng"}[*b.LastDrillOk], *b.LastDrillAgeH)
		it.Status = grade(*b.LastDrillOk, ReadinessFail)
	}
	return it
}

// urgentOpen đếm cảnh báo gấp đã gửi trong 24 giờ qua: alert_events chỉ ghi "đã gửi", không có trạng thái đã xử lý.
func urgentOpen(alerts []observability.AlertSummary) ReadinessItem {
	n := 0
	for _, a := range alerts {
		if a.Severity == "urgent" {
			n++
		}
	}
	return ReadinessItem{
		Key: "platform.urgent_open", Group: groupSystem, Status: grade(n == 0, ReadinessFail),
		Value: fmt.Sprint(n), Target: "0 cảnh báo gấp trong 24 giờ qua",
	}
}
