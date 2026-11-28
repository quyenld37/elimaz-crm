package carrier_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/testkit"
)

// postMany gửi một gói nhiều sự kiện tới tuyến của tài khoản (ký đúng); trả mã HTTP.
func (e *hookEnv) postMany(evs []fake.Event) int {
	e.t.Helper()
	body, err := json.Marshal(fake.Packet{Events: evs})
	if err != nil {
		e.t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(e.t.Context(), http.MethodPost, e.srv.URL+"/hooks/fake/"+e.rk, bytes.NewReader(body))
	req.Header.Set(fake.SignatureHeader, fake.Sign(body, hookSecret))
	req.Header.Set("Content-Type", "application/json")
	status, _, _ := testkit.Do(e.t, req)
	return status
}

// drainMany như drain với lượt nhận 100 việc (testkit.DrainDue tối đa 50 lượt × 10 việc, không đủ cho 1.000 việc).
func (e *hookEnv) drainMany() {
	e.t.Helper()
	for range 100 {
		n, err := e.worker.RunOnce(e.t.Context(), 100)
		if err != nil {
			e.t.Fatalf("RunOnce: %v", err)
		}
		if n == 0 && testkit.MakeDueSoon(e.t, e.staff.Owner) == 0 {
			return
		}
	}
	e.t.Fatal("worker chưa hết việc sau 100 lượt")
}

// noDeadNoAlert: không việc nào dead, không cảnh báo nào được ghi.
func (e *hookEnv) noDeadNoAlert() {
	e.t.Helper()
	if n := e.staff.Count(e.t, "SELECT count(*) FROM platform.jobs WHERE status = 'dead'"); n != 0 {
		e.t.Fatalf("việc dead = %d, muốn 0", n)
	}
	if n := e.staff.Count(e.t, "SELECT count(*) FROM platform.alert_events"); n != 0 {
		e.t.Fatalf("cảnh báo = %d, muốn 0", n)
	}
}

// MO07 (phần máy), Task 265: webhook của đơn cũ (mã VTP0000OLD1 không có trong CRM, tài khoản không có vận đơn chờ mã)
// qua ingest thật → việc vào done ngay lần đầu, 1 dòng unmatched event_count 1; hãng gửi lại → 2; không lệnh tra,
// không việc dead, không cảnh báo; carrier_event_unmatched +2. Vận đơn của CRM trong cùng tài khoản vẫn có lệnh tra
// như M3 (VC05).
func TestWebhookRouted_OldOrderUnmatched(t *testing.T) {
	e := newHookEnv(t)
	v1 := e.shipment("FK0001")
	for i, id := range []string{"old-1", "old-1-again"} {
		if s := e.post("/"+e.rk, hookSecret, event(id, "VTP0000OLD1", "delivering", i)); s != http.StatusOK {
			t.Fatalf("POST %d = %d", i, s)
		}
		e.drain()
		if n := e.countIn(`SELECT coalesce(sum(event_count), 0)::int FROM fulfillment.unmatched_carrier_events
			WHERE tracking_code = 'VTP0000OLD1' AND carrier_key = 'fake' AND carrier_account_id = $1`, e.shop.Account); n != i+1 {
			t.Fatalf("event_count sau lần %d = %d", i+1, n)
		}
	}
	if n := e.countIn("SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.fake' AND status = 'done' AND attempts = 1"); n != 2 {
		t.Fatalf("việc vào done lần đầu = %d, muốn 2", n)
	}
	if n := e.countIn("SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.refresh_status'"); n != 0 {
		t.Fatalf("lệnh tra = %d, muốn 0", n)
	}
	if n := e.metric("carrier_event_unmatched"); n != 2 {
		t.Fatalf("carrier_event_unmatched = %d, muốn 2", n)
	}
	e.noDeadNoAlert()
	if s := e.post("/"+e.rk, hookSecret, event("crm-1", "FK0001", "delivered", 5)); s != http.StatusOK {
		t.Fatalf("POST vận đơn CRM = %d", s)
	}
	e.drain()
	if n := e.sigRefresh(v1.ID); n != 1 {
		t.Fatalf("lệnh tra V1 = %d, muốn 1", n)
	}
	if n := e.countIn("SELECT count(*) FROM fulfillment.unmatched_carrier_events WHERE tracking_code = 'FK0001'"); n != 0 {
		t.Fatalf("vận đơn CRM bị ghi unmatched: %d", n)
	}
}

// 1.000 sự kiện ngoài hệ thống liên tiếp (một gói) → 1.000 việc vào done, 1.000 dòng unmatched, 0 dead, 0 cảnh báo.
func TestWebhookRouted_ThousandOldEvents(t *testing.T) {
	e := newHookEnv(t)
	evs := make([]fake.Event, 0, 1000)
	for i := range 1000 {
		evs = append(evs, fake.Event{
			EventID: fmt.Sprintf("bulk-%d", i), AccountKey: "fake-1", Tracking: fmt.Sprintf("VTPOLD%05d", i),
			Status: "delivering", SentAt: time.Unix(int64(1_795_510_800+i), 0),
		})
	}
	if s := e.postMany(evs); s != http.StatusOK {
		t.Fatalf("POST = %d", s)
	}
	e.drainMany()
	if n := e.countIn("SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.fake' AND status = 'done'"); n != 1000 {
		t.Fatalf("việc vào done = %d, muốn 1000", n)
	}
	if n := e.countIn("SELECT count(*) FROM fulfillment.unmatched_carrier_events"); n != 1000 {
		t.Fatalf("dòng unmatched = %d", n)
	}
	e.noDeadNoAlert()
}
