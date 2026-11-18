package webhooks_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/selftest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// enqueueAs gọi EnqueueInbound trong một transaction tenant của crm_app (như lệnh hỏi bù của M4).
func (e *splitEnv) enqueueAs(t *testing.T, tenant uuid.UUID, ev webhooks.InboundEvent, receivedAt time.Time, via string) (bool, error) {
	t.Helper()
	var created bool
	err := tenancy.WithTenant(t.Context(), e.App, tenant, func(tx pgx.Tx) error {
		var err error
		created, err = webhooks.EnqueueInbound(t.Context(), tx, queue.NewQueue(), tenant, selftest.Key, ev, receivedAt, via)
		return err //nolint:wrapcheck // test
	})
	return created, err //nolint:wrapcheck // test
}

func pingEvent(id string, occurred time.Time) webhooks.InboundEvent {
	data, _ := json.Marshal(ping(id, "ka", occurred)) //nolint:errchkjson // struct đơn giản
	return webhooks.InboundEvent{Type: "ping", DedupeKey: id, OrderKey: "conv-1", OccurredAt: occurred, Payload: data}
}

// EnqueueInbound (M4: hỏi bù, đọc comment): cùng sự kiện hai lần (kể cả khác via) → created true rồi false, một việc;
// payload chỉ mang mã bản nhận cùng receivedAt, via (không nội dung); handler đọc lại đúng nội dung, ReceivedAt, Via;
// OrderKey, OrderSeq như SplitHandler.
func TestEnqueueInboundDedupesAndDelivers(t *testing.T) {
	e := newSplitEnv(t, 0)
	occurred := at(time.Second)
	received := at(5 * time.Second)
	ev := pingEvent("bf-1", occurred)
	for i, via := range []string{webhooks.ViaBackfill, webhooks.ViaBackfill, webhooks.ViaCommentPoll} {
		created, err := e.enqueueAs(t, e.a, ev, received, via)
		if err != nil || created != (i == 0) {
			t.Fatalf("lần %d (%s): created %v err %v", i+1, via, created, err)
		}
	}
	jobs := e.jobs(t, webhooks.InboundKind(selftest.Key).Name)
	if len(jobs) != 1 || jobs[0].tenant == nil || *jobs[0].tenant != e.a {
		t.Fatalf("việc vào: %+v", jobs)
	}
	var p map[string]any
	if err := json.Unmarshal(jobs[0].payload, &p); err != nil {
		t.Fatal(err)
	}
	if p["via"] != webhooks.ViaBackfill || p["receivedAt"] != received.Format(time.RFC3339Nano) || p["dedupeKey"] != "bf-1" ||
		p["deliveryId"] == nil || p["data"] != nil || p["payload"] != nil {
		t.Fatalf("payload việc: %s", jobs[0].payload)
	}
	var orderKey string
	var orderSeq int64
	if err := e.Owner.Pool.QueryRow(t.Context(), "SELECT order_key, order_seq FROM platform.jobs WHERE id = $1",
		jobs[0].id).Scan(&orderKey, &orderSeq); err != nil {
		t.Fatal(err)
	}
	if orderKey != "inbound.selftest:conv-1" || orderSeq != occurred.UnixMicro() {
		t.Fatalf("order_key %q order_seq %d", orderKey, orderSeq)
	}
	e.drain(t)
	calls := e.rec.calls()
	if len(calls) != 1 {
		t.Fatalf("handler nhận %d sự kiện, muốn 1", len(calls))
	}
	c := calls[0]
	if c.Type != "ping" || !c.OccurredAt.Equal(occurred) || !c.ReceivedAt.Equal(received) || c.Via != webhooks.ViaBackfill ||
		c.TenantID != e.a || string(c.Data) != string(ev.Payload) {
		t.Fatalf("sự kiện vào: %+v", c)
	}
}

// Sự kiện đã vào qua webhook (SplitHandler) rồi hỏi bù cùng DedupeKey → created false, không việc thứ hai.
func TestEnqueueInboundAfterWebhookIsDuplicate(t *testing.T) {
	e := newSplitEnv(t, 0)
	e.deliver(t, packet(t, ping("w-1", "ka", at(0))))
	e.drain(t)
	created, err := e.enqueueAs(t, e.a, pingEvent("w-1", at(0)), at(time.Minute), webhooks.ViaBackfill)
	if err != nil || created {
		t.Fatalf("hỏi bù sự kiện đã có: created %v err %v", created, err)
	}
	if jobs := e.jobs(t, webhooks.InboundKind(selftest.Key).Name); len(jobs) != 1 {
		t.Fatalf("có %d việc vào, muốn 1", len(jobs))
	}
}

// via ngoài sáu giá trị, tenant rỗng, nguồn rỗng, giờ nhận rỗng → ErrInvalidEvent, không ghi gì.
func TestEnqueueInboundRejects(t *testing.T) {
	e := newSplitEnv(t, 0)
	ev := pingEvent("x-1", at(0))
	for name, call := range map[string]func(tx pgx.Tx) error{
		"via lạ": func(tx pgx.Tx) error {
			_, err := webhooks.EnqueueInbound(t.Context(), tx, queue.NewQueue(), e.a, selftest.Key, ev, at(0), "email")
			return err //nolint:wrapcheck // test
		},
		"tenant rỗng": func(tx pgx.Tx) error {
			_, err := webhooks.EnqueueInbound(t.Context(), tx, queue.NewQueue(), uuid.Nil, selftest.Key, ev, at(0), webhooks.ViaHistory)
			return err //nolint:wrapcheck // test
		},
		"nguồn rỗng": func(tx pgx.Tx) error {
			_, err := webhooks.EnqueueInbound(t.Context(), tx, queue.NewQueue(), e.a, "", ev, at(0), webhooks.ViaLivePoll)
			return err //nolint:wrapcheck // test
		},
		"giờ nhận rỗng": func(tx pgx.Tx) error {
			_, err := webhooks.EnqueueInbound(t.Context(), tx, queue.NewQueue(), e.a, selftest.Key, ev, time.Time{}, webhooks.ViaLiveStream)
			return err //nolint:wrapcheck // test
		},
	} {
		err := tenancy.WithTenant(t.Context(), e.App, e.a, call)
		if !errors.Is(err, webhooks.ErrInvalidEvent) {
			t.Errorf("%s: %v, muốn ErrInvalidEvent", name, err)
		}
	}
	var n int
	if err := e.Owner.Pool.QueryRow(t.Context(), "SELECT count(*) FROM platform.jobs").Scan(&n); err != nil || n != 0 {
		t.Fatalf("có %d việc, muốn 0 (%v)", n, err)
	}
}
