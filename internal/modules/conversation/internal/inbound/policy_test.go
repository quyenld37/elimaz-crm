package inbound_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/inbound"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

// Sự kiện policy.enforcement (Task 150; 02 mục 8): Meta chặn Page → degraded meta_policy:block, đúng một lệnh cảnh báo
// gấp meta-policy:<id> (payload có tên Page, hành động, không nội dung), một báo hiệu channel.status; sự kiện lặp lại
// không thêm cảnh báo; Page lạ → bỏ qua không lỗi; dữ liệu hỏng → lỗi vĩnh viễn.
func TestHandlePolicy(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	tenant := s.Tenant(t, "A")
	id := s.ChannelAccount(t, tenant, "facebook", "1001", "Shop Một")
	bus := &realtimetest.RecordingBus{}
	d := deps.Deps{DB: s.App, Queue: queue.NewQueue(), Bus: bus}
	handle := func(data string) error {
		t.Helper()
		return tenancy.WithTenant(t.Context(), s.App, tenant, func(tx pgx.Tx) error {
			return inbound.HandlePolicy(t.Context(), tx, d, "facebook", webhooks.Inbound{
				Type: channel.EventPolicyEnforcement, Data: json.RawMessage(data), TenantID: tenant, JobID: uuid.New(),
				ReceivedAt: time.Now(), Via: webhooks.ViaWebhook,
			})
		})
	}
	fixture := `{"PageID":"1001","Action":"block","Reason":"Spam phát hiện bởi Meta"}`
	for range 2 {
		if err := handle(fixture); err != nil {
			t.Fatal(err)
		}
	}
	var status, reason string
	s.Exec(t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT status, coalesce(status_reason, '') FROM channel.channel_accounts WHERE id = $1`, id).
			Scan(&status, &reason)
	})
	if status != channel.StatusDegraded || reason != "meta_policy:block" {
		t.Fatalf("trạng thái = %s %s", status, reason)
	}
	rows, err := s.Owner.Pool.Query(t.Context(), `SELECT payload::text FROM platform.jobs WHERE tenant_id = $1 AND kind = $2
		AND dedupe_key LIKE 'meta-policy:' || $3 || ':%'`, tenant, channel.PolicyAlertKind, id.String())
	if err != nil {
		t.Fatal(err)
	}
	ps, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(ps) != 1 || !strings.Contains(ps[0], "Shop Một") || !strings.Contains(ps[0], "block") ||
		strings.Contains(ps[0], "Spam") {
		t.Fatalf("lệnh cảnh báo = %v, %v", ps, err)
	}
	if n := len(bus.Signals()); n != 1 || bus.Signals()[0].Topic != channel.TopicStatus {
		t.Fatalf("báo hiệu = %+v", bus.Signals())
	}
	if err := handle(`{"PageID":"9999","Action":"block"}`); err != nil {
		t.Fatalf("Page lạ: %v", err)
	}
	if err := handle(`{"PageID":`); !errors.Is(err, queue.ErrPermanent) {
		t.Fatalf("dữ liệu hỏng: %v", err)
	}
}
