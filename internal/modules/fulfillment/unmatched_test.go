package fulfillment_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// unmatchedCount trả event_count của dòng (tài khoản hãng của tuyến, mã) trong bảng sự kiện ngoài hệ thống; 0 khi
// chưa có dòng.
func (e *shipEnv) unmatchedCount(account uuid.UUID, tracking string) int {
	e.t.Helper()
	return e.count(`SELECT coalesce(sum(event_count), 0)::int FROM fulfillment.unmatched_carrier_events
		WHERE carrier_account_id = $1 AND tracking_code = $2`, account, tracking)
}

// carrierCalls là tổng lời gọi API của hãng giả (tạo, tra, hủy, đổi địa chỉ).
func (e *shipEnv) carrierCalls() int {
	n := 0
	for _, op := range []string{"create", "lookup", "cancel", "update_address"} {
		n += e.carrier.Calls(op)
	}
	return n
}

// MO07 (phần máy), nhánh a: mã VTP0000OLD1 không có trong CRM, tài khoản không có vận đơn chờ mã hãng → xong ngay lượt
// đầu (không lỗi), 1 dòng event_count = 1; gửi lại → 2; không lệnh tra, không gọi hãng; carrier_event_unmatched +2.
func TestUnmatched_OldOrderRecordedAtOnce(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	reg := metrics.NewRegistry()
	rk := e.routeKey(e.account)
	calls := e.carrierCalls()
	for i, key := range []string{"old-1", "old-1-resend"} {
		sig := fulfillment.Signal{RouteKey: rk, EventKey: key, TrackingCode: "VTP0000OLD1", ShipmentCode: "NHANHREF77"}
		if err := e.trySignal(sig, reg); err != nil {
			t.Fatalf("tín hiệu %d = %v, muốn nil ngay lượt đầu", i+1, err)
		}
		if n := e.unmatchedCount(e.account, "VTP0000OLD1"); n != i+1 {
			t.Fatalf("event_count sau lần %d = %d", i+1, n)
		}
	}
	if n := e.count("SELECT count(*) FROM fulfillment.unmatched_carrier_events"); n != 1 {
		t.Fatalf("số dòng = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.unmatched_carrier_events WHERE carrier_key = 'fake'
		AND last_raw_status IS NULL AND first_seen_at <= last_seen_at`); n != 1 {
		t.Fatalf("dòng thiếu hãng hay mốc giờ sai: %d", n)
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.refresh_status'"); n != 0 {
		t.Fatalf("lệnh tra = %d, muốn 0", n)
	}
	if n := e.carrierCalls() - calls; n != 0 {
		t.Fatalf("gọi hãng %d lần, muốn 0", n)
	}
	if n := metricCount(reg, fulfillment.MetricCarrierEventUnmatched, "fake"); n != 2 {
		t.Fatalf("%s = %d, muốn 2", fulfillment.MetricCarrierEventUnmatched, n)
	}
}

// VC10 giữ nguyên, nhánh b: tài khoản có một vận đơn chờ mã hãng → tín hiệu lạ ở lượt thường là lỗi tạm, không ghi
// unmatched; ở lượt thử cuối → ghi unmatched, không lỗi (việc done thay vì dead). Tín hiệu mang mã CRM (E…) không có
// vận đơn: lỗi tạm ở lượt thường cả khi tài khoản không có vận đơn chờ mã; lượt cuối ghi unmatched.
func TestUnmatched_PendingAccountLastAttempt(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	o, _ := e.order(1)
	pending := e.mustCreateShipment(o.ID)
	reg := metrics.NewRegistry()
	a := hookCarrier{Carrier: e.carrier}
	sig := fulfillment.Signal{RouteKey: e.routeKey(e.account), EventKey: "old-2", TrackingCode: "VTP0000OLD2"}
	for attempt := 1; attempt < 8; attempt++ {
		err := e.trySignalAt(a, sig, reg, attempt, false)
		if !errors.Is(err, fulfillment.ErrShipmentNotYetKnown) || errors.Is(err, queue.ErrPermanent) {
			t.Fatalf("lần %d = %v, muốn ErrShipmentNotYetKnown tạm", attempt, err)
		}
	}
	if n := e.unmatchedCount(e.account, "VTP0000OLD2"); n != 0 {
		t.Fatalf("ghi unmatched trước lượt cuối: %d", n)
	}
	if err := e.trySignalAt(a, sig, reg, 8, true); err != nil {
		t.Fatalf("lượt cuối = %v, muốn nil", err)
	}
	if n := e.unmatchedCount(e.account, "VTP0000OLD2"); n != 1 {
		t.Fatalf("event_count sau lượt cuối = %d, muốn 1", n)
	}
	if n := e.refreshJobs(pending.ID); n != 0 {
		t.Fatalf("lệnh tra cho vận đơn chờ mã = %d", n)
	}
	if n := metricCount(reg, fulfillment.MetricCarrierEventUnmatched, "fake"); n != 1 {
		t.Fatalf("chỉ số = %d, muốn 1", n)
	}

	// Vận đơn chờ mã đã kết thúc (hủy): tài khoản không còn vận đơn chờ → tín hiệu mã hãng lạ ghi ngay.
	e.ownerExec("UPDATE fulfillment.shipments SET status = 'cancelled', cancel_kind = 'manual' WHERE id = $1", pending.ID)
	if err := e.trySignal(fulfillment.Signal{RouteKey: sig.RouteKey, EventKey: "old-3", TrackingCode: "VTP0000OLD3"}, nil); err != nil {
		t.Fatalf("sau khi vận đơn chờ đã hủy = %v, muốn nil", err)
	}
	crm := fulfillment.Signal{RouteKey: sig.RouteKey, EventKey: "crm-1", ShipmentCode: "E261128ABCDEF"}
	if err := e.trySignal(crm, nil); !errors.Is(err, fulfillment.ErrShipmentNotYetKnown) {
		t.Fatalf("mã CRM chưa có vận đơn = %v, muốn ErrShipmentNotYetKnown", err)
	}
	if err := e.trySignalAt(a, crm, nil, 8, true); err != nil {
		t.Fatalf("mã CRM lượt cuối = %v", err)
	}
	if n := e.unmatchedCount(e.account, "E261128ABCDEF"); n != 1 {
		t.Fatalf("mã CRM lượt cuối: event_count %d, muốn 1", n)
	}
}

// 1.000 sự kiện ngoài hệ thống liên tiếp → đều xong, 1.000 dòng, tổng event_count 1.000; không lệnh tra.
func TestUnmatched_ThousandEvents(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	rk := e.routeKey(e.account)
	for i := range 1000 {
		sig := fulfillment.Signal{RouteKey: rk, EventKey: fmt.Sprintf("bulk-%d", i), TrackingCode: fmt.Sprintf("VTPOLD%05d", i)}
		if err := e.trySignal(sig, nil); err != nil {
			t.Fatalf("sự kiện %d = %v", i, err)
		}
	}
	if n := e.count("SELECT count(*) FROM fulfillment.unmatched_carrier_events"); n != 1000 {
		t.Fatalf("dòng = %d", n)
	}
	if n := e.count("SELECT coalesce(sum(event_count), 0)::int FROM fulfillment.unmatched_carrier_events"); n != 1000 {
		t.Fatalf("tổng event_count = %d", n)
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.refresh_status'"); n != 0 {
		t.Fatalf("lệnh tra = %d", n)
	}
}

// newUnmatchedEnv dựng môi trường vận đơn và ghi bốn sự kiện ngoài hệ thống từ mốc t0 (trả về): OLD-A (fake, hai lần:
// t0 và t0+48h, trạng thái cuối 501), OLD-B (fake, t0+1h), OLD-C (vtp, t0+2h, không tài khoản).
func newUnmatchedEnv(t *testing.T) (*shipEnv, time.Time) {
	t.Helper()
	e := newShipEnv(t)
	t0 := time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC)
	acc := e.account
	if err := e.app(func(tx pgx.Tx) error {
		for i, c := range []struct {
			carrier, code, raw string
			at                 time.Time
			account            *uuid.UUID
		}{
			{"fake", "OLD-A", "", t0, &acc},
			{"fake", "OLD-B", "200", t0.Add(time.Hour), &acc},
			{"vtp", "OLD-C", "", t0.Add(2 * time.Hour), nil},
			{"fake", "OLD-A", "501", t0.Add(48 * time.Hour), &acc},
		} {
			if err := fulfillment.RecordUnmatched(t.Context(), tx, c.carrier, c.account, c.code, c.raw, c.at); err != nil {
				return fmt.Errorf("ghi %d: %w", i, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return e, t0
}

// listUnmatched đọc một trang ListUnmatched trong ngữ cảnh tenant của e.
func listUnmatched(e *shipEnv, p fulfillment.UnmatchedPage) ([]fulfillment.UnmatchedEvent, *uuid.UUID, error) {
	var (
		rows []fulfillment.UnmatchedEvent
		next *uuid.UUID
	)
	err := e.app(func(tx pgx.Tx) (err error) {
		rows, next, err = fulfillment.ListUnmatched(e.t.Context(), tx, p)
		return err //nolint:wrapcheck // test
	})
	return rows, next, err
}

// unmatchedCodes là "mã/hãng/số lần " của từng dòng.
func unmatchedCodes(rows []fulfillment.UnmatchedEvent) string {
	s := ""
	for _, r := range rows {
		s += fmt.Sprintf("%s/%s/%d ", r.TrackingCode, r.CarrierKey, r.EventCount)
	}
	return s
}

// RecordUnmatched lưu mốc giờ, trạng thái thô cuối, tài khoản; ListUnmatched: mới thấy trước, con trỏ.
func TestUnmatched_RecordAndList(t *testing.T) {
	t.Parallel()
	e, t0 := newUnmatchedEnv(t)
	all, next, err := listUnmatched(e, fulfillment.UnmatchedPage{Limit: 10})
	if err != nil || next != nil || unmatchedCodes(all) != "OLD-C/vtp/1 OLD-B/fake/1 OLD-A/fake/2 " {
		t.Fatalf("tất cả = %q next %v err %v", unmatchedCodes(all), next, err)
	}
	a := all[2]
	if !a.FirstSeenAt.Equal(t0) || !a.LastSeenAt.Equal(t0.Add(48*time.Hour)) || a.LastRawStatus != "501" {
		t.Fatalf("dòng OLD-A: %+v", a)
	}
	if a.CarrierAccountID == nil || *a.CarrierAccountID != e.account || all[0].CarrierAccountID != nil {
		t.Fatalf("tài khoản OLD-A: %v; OLD-C: %v", a.CarrierAccountID, all[0].CarrierAccountID)
	}
	first, next, err := listUnmatched(e, fulfillment.UnmatchedPage{Limit: 2})
	if err != nil || next == nil || unmatchedCodes(first) != "OLD-C/vtp/1 OLD-B/fake/1 " {
		t.Fatalf("trang 1 = %q next %v err %v", unmatchedCodes(first), next, err)
	}
	second, next2, err := listUnmatched(e, fulfillment.UnmatchedPage{Limit: 2, After: next})
	if err != nil || next2 != nil || unmatchedCodes(second) != "OLD-A/fake/2 " {
		t.Fatalf("trang 2 = %q next %v err %v", unmatchedCodes(second), next2, err)
	}
}

// ListUnmatched: lọc hãng, khoảng thời gian (có sự kiện trong khoảng); con trỏ lạ, cỡ trang sai, khoảng ngược →
// ErrInvalidPage; tenant khác không thấy.
func TestUnmatched_ListFilters(t *testing.T) {
	t.Parallel()
	e, t0 := newUnmatchedEnv(t)
	for name, c := range map[string]struct {
		p    fulfillment.UnmatchedPage
		want string
	}{
		"lọc hãng": {fulfillment.UnmatchedPage{Limit: 10, Carrier: "fake"}, "OLD-B/fake/1 OLD-A/fake/2 "},
		// [t0+30 phút, t0+90 phút): OLD-B (thấy lúc t0+1h), OLD-A (t0 … t0+48h, phủ khoảng); không OLD-C.
		"khoảng": {
			fulfillment.UnmatchedPage{Limit: 10, Since: t0.Add(30 * time.Minute), Until: t0.Add(90 * time.Minute)},
			"OLD-B/fake/1 OLD-A/fake/2 ",
		},
		"từ t0+3h": {fulfillment.UnmatchedPage{Limit: 10, Since: t0.Add(3 * time.Hour)}, "OLD-A/fake/2 "},
	} {
		if rows, _, err := listUnmatched(e, c.p); err != nil || unmatchedCodes(rows) != c.want {
			t.Errorf("%s = %q %v, muốn %q", name, unmatchedCodes(rows), err, c.want)
		}
	}
	stranger := uuid.New()
	for name, p := range map[string]fulfillment.UnmatchedPage{
		"cỡ 0":         {Limit: 0},
		"cỡ 101":       {Limit: 101},
		"con trỏ lạ":   {Limit: 5, After: &stranger},
		"khoảng ngược": {Limit: 5, Since: t0.Add(time.Hour), Until: t0},
	} {
		if _, _, err := listUnmatched(e, p); !errors.Is(err, fulfillment.ErrInvalidPage) {
			t.Errorf("%s = %v, muốn ErrInvalidPage", name, err)
		}
	}
	other := e.staff.Tenant(t, "B")
	var n int
	if err := tenancy.WithTenant(t.Context(), e.staff.App, other, func(tx pgx.Tx) error {
		rows, _, err := fulfillment.ListUnmatched(t.Context(), tx, fulfillment.UnmatchedPage{Limit: 10})
		n = len(rows)
		return err //nolint:wrapcheck // test
	}); err != nil || n != 0 {
		t.Fatalf("tenant B thấy %d dòng, err %v", n, err)
	}
}
