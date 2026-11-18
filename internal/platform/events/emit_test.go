package events_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// env: database thử đã migrate (testkit.Staff: pool crm_app như tiến trình api, worker và pool chủ bảng) với một
// tenant.
type env struct {
	*testkit.Staff
	q      queue.JobQueue
	tenant uuid.UUID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	s := testkit.OpenStaff(t)
	return &env{Staff: s, q: queue.NewQueue(), tenant: s.Tenant(t, "a")}
}

// countEvents đếm sự kiện của tenant thử (bảng ép RLS, kể cả với chủ bảng: đọc trong WithTenant).
func (e *env) countEvents(t *testing.T, where string, args ...any) int {
	t.Helper()
	var n int
	e.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT count(*) FROM platform.domain_events WHERE "+where, args...).Scan(&n)
	})
	return n
}

// emit gọi r.Emit trong WithTenant của tenant thử; fail != nil thì lùi transaction bằng lỗi đó.
func (e *env) emit(t *testing.T, r *events.Registry, ev events.Event, fail error) (uuid.UUID, int, error) {
	t.Helper()
	var (
		id   uuid.UUID
		jobs int
	)
	err := tenancy.WithTenant(t.Context(), e.App, ev.TenantID, func(tx pgx.Tx) error {
		var err error
		id, jobs, err = r.Emit(t.Context(), tx, e.q, ev)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		return fail
	})
	return id, jobs, err //nolint:wrapcheck // test
}

// consumer thử: ghi tên topic theo thứ tự nhận; fail quyết định lỗi.
type consumer struct {
	name   string
	topics []string
	fail   func(e events.StoredEvent, call int) error

	mu    sync.Mutex
	got   []string
	calls map[uuid.UUID]int
}

func newConsumer(name string, topics ...string) *consumer {
	return &consumer{name: name, topics: topics, calls: map[uuid.UUID]int{}}
}

func (c *consumer) Name() string     { return c.name }
func (c *consumer) Topics() []string { return c.topics }

func (c *consumer) Handle(_ context.Context, e events.StoredEvent, _ pgx.Tx) error {
	c.mu.Lock()
	c.calls[e.ID]++
	n := c.calls[e.ID]
	c.mu.Unlock()
	if c.fail != nil {
		if err := c.fail(e, n); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.got = append(c.got, e.Topic)
	c.mu.Unlock()
	return nil
}

func (c *consumer) handled() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.got)
}

func (c *consumer) callsOf(id uuid.UUID) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[id]
}

func registry(t *testing.T, cs ...events.Consumer) *events.Registry {
	t.Helper()
	r := events.NewRegistry()
	for _, c := range cs {
		if err := r.Register(c); err != nil {
			t.Fatalf("Register(%s): %v", c.Name(), err)
		}
	}
	return r
}

func newWorker(e *env, r *events.Registry, concurrency int) *queue.Worker {
	return queue.NewWorker(e.App, r.JobHandlers(), queue.WorkerOptions{WorkerID: "w", Concurrency: concurrency})
}

// drain gọi RunOnce tới khi không còn việc nhận được, đưa việc hẹn thử lại về hạn mỗi vòng (tối đa 30 vòng).
func (e *env) drain(t *testing.T, w *queue.Worker) {
	t.Helper()
	for range 30 {
		if _, err := e.Owner.Pool.Exec(t.Context(), "UPDATE platform.jobs SET run_at = now() WHERE status = 'pending'"); err != nil {
			t.Fatal(err)
		}
		n, err := w.RunOnce(t.Context(), 10)
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if n == 0 {
			return
		}
	}
}

func (e *env) ev(seq int64, topic string) events.Event {
	return events.Event{
		TenantID: e.tenant, AggregateType: "order", AggregateID: "o-1", Seq: seq, Topic: topic,
		Payload: map[string]string{"orderId": "o-1"},
	}
}

// emitReturnedThenDelivered phát Seq 2 (order.returned) rồi Seq 1 (order.delivered) của cùng đơn, mỗi cái một
// transaction (ngược thứ tự của đối tượng).
func (e *env) emitReturnedThenDelivered(t *testing.T, r *events.Registry) {
	t.Helper()
	for _, ev := range []events.Event{e.ev(2, "order.returned"), e.ev(1, "order.delivered")} {
		if _, _, err := e.emit(t, r, ev, nil); err != nil {
			t.Fatal(err)
		}
	}
}

// Emit trong transaction lùi → 0 sự kiện, 0 việc.
func TestEmitRolledBackLeavesNothing(t *testing.T) {
	e := newEnv(t)
	r := registry(t, newConsumer("a", "order.delivered"))
	boom := errors.New("lùi")
	if _, _, err := e.emit(t, r, e.ev(1, "order.delivered"), boom); !errors.Is(err, boom) {
		t.Fatalf("muốn lỗi lùi, nhận %v", err)
	}
	if n := e.countEvents(t, "true"); n != 0 {
		t.Fatalf("sự kiện còn %d", n)
	}
	if n := e.Count(t, "SELECT count(*) FROM platform.jobs"); n != 0 {
		t.Fatalf("việc còn %d", n)
	}
}

// Hai consumer cùng topic → 2 việc đúng kind, khóa, thứ tự; consumer không nghe topic → 0 việc cho nó.
func TestEmitOneJobPerListeningConsumer(t *testing.T) {
	e := newEnv(t)
	r := registry(t, newConsumer("a", "order.delivered"), newConsumer("b", "order.returned", "order.delivered"),
		newConsumer("c", "order.returned"))
	id, jobs, err := e.emit(t, r, e.ev(7, "order.delivered"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if jobs != 2 {
		t.Fatalf("jobs = %d, muốn 2", jobs)
	}
	for _, name := range []string{"a", "b"} {
		n := e.Count(t, `SELECT count(*) FROM platform.jobs WHERE kind = $1 AND tenant_id = $2 AND dedupe_key = $3
			AND order_key = $4 AND order_seq = 7 AND block_on_dead AND payload = jsonb_build_object('eventId', $3::text)`,
			"event."+name, e.tenant, id.String(), name+":order:o-1")
		if n != 1 {
			t.Fatalf("consumer %s: %d việc đúng dạng, muốn 1", name, n)
		}
	}
	if n := e.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = 'event.c'"); n != 0 {
		t.Fatalf("consumer c không nghe mà có %d việc", n)
	}
	if n := e.countEvents(t, `id = $1 AND tenant_id = $2 AND aggregate_type = 'order'
		AND aggregate_id = 'o-1' AND seq = 7 AND topic = 'order.delivered' AND payload = '{"orderId":"o-1"}'`, id, e.tenant); n != 1 {
		t.Fatalf("dòng sự kiện sai: %d", n)
	}
}

// Không consumer nào nghe → sự kiện vẫn được ghi, 0 việc.
func TestEmitWithoutConsumersStoresEvent(t *testing.T) {
	e := newEnv(t)
	_, jobs, err := e.emit(t, events.NewRegistry(), e.ev(1, "order.created"), nil)
	if err != nil || jobs != 0 {
		t.Fatalf("jobs %d, err %v", jobs, err)
	}
	if n := e.countEvents(t, "true"); n != 1 {
		t.Fatalf("sự kiện %d, muốn 1", n)
	}
}

// B lỗi lần đầu → A done và không bị gọi lại; B thử lại rồi done.
func TestConsumerFailureIsIndependent(t *testing.T) {
	e := newEnv(t)
	a := newConsumer("a", "order.delivered")
	b := newConsumer("b", "order.delivered")
	b.fail = func(_ events.StoredEvent, call int) error {
		if call == 1 {
			return errors.New("lỗi tạm")
		}
		return nil
	}
	r := registry(t, a, b)
	id, _, err := e.emit(t, r, e.ev(1, "order.delivered"), nil)
	if err != nil {
		t.Fatal(err)
	}
	e.drain(t, newWorker(e, r, 2))
	if a.callsOf(id) != 1 || b.callsOf(id) != 2 {
		t.Fatalf("số lần gọi a=%d b=%d, muốn 1 và 2", a.callsOf(id), b.callsOf(id))
	}
	if n := e.Count(t, "SELECT count(*) FROM platform.jobs WHERE status = 'done'"); n != 2 {
		t.Fatalf("việc done %d, muốn 2", n)
	}
	if n := e.Count(t, "SELECT attempts FROM platform.jobs WHERE kind = 'event.b'"); n != 2 {
		t.Fatalf("attempts của b %d, muốn 2", n)
	}
}

// Cùng AggregateID, Emit Seq 2 (returned) rồi Seq 1 (delivered), mỗi cái một transaction; worker Concurrency 4 →
// consumer nhận delivered trước.
func TestEventsRunInSeqOrder(t *testing.T) {
	for i := range 5 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			e := newEnv(t)
			c := newConsumer("aff", "order.delivered", "order.returned")
			r := registry(t, c)
			e.emitReturnedThenDelivered(t, r)
			e.drain(t, newWorker(e, r, 4))
			if got := c.handled(); !slices.Equal(got, []string{"order.delivered", "order.returned"}) {
				t.Fatalf("thứ tự %v", got)
			}
		})
	}
}

// Consumer lỗi ở delivered tới dead → returned không bao giờ chạy.
func TestDeadEventBlocksLaterEvents(t *testing.T) {
	e := newEnv(t)
	c := newConsumer("aff", "order.delivered", "order.returned")
	c.fail = func(ev events.StoredEvent, _ int) error {
		if ev.Topic == "order.delivered" {
			return errors.New("luôn lỗi")
		}
		return nil
	}
	r := registry(t, c)
	e.emitReturnedThenDelivered(t, r)
	e.drain(t, newWorker(e, r, 4))
	if n := e.Count(t, "SELECT count(*) FROM platform.jobs WHERE order_seq = 1 AND status = 'dead'"); n != 1 {
		t.Fatalf("delivered phải dead sau 8 lần")
	}
	if n := e.Count(t, "SELECT count(*) FROM platform.jobs WHERE order_seq = 2 AND status = 'pending' AND attempts = 0"); n != 1 {
		t.Fatalf("returned phải còn pending, chưa chạy lần nào")
	}
	if got := c.handled(); len(got) != 0 {
		t.Fatalf("consumer không được nhận sự kiện nào thành công: %v", got)
	}
}

// Emit trùng (aggregate, seq) → ErrDuplicateEvent.
func TestEmitDuplicateSeq(t *testing.T) {
	e := newEnv(t)
	r := events.NewRegistry()
	if _, _, err := e.emit(t, r, e.ev(1, "order.delivered"), nil); err != nil {
		t.Fatal(err)
	}
	_, _, err := e.emit(t, r, e.ev(1, "order.returned"), nil)
	if !errors.Is(err, events.ErrDuplicateEvent) {
		t.Fatalf("muốn ErrDuplicateEvent, nhận %v", err)
	}
	// Cùng seq nhưng đối tượng khác → được.
	other := e.ev(1, "order.delivered")
	other.AggregateID = "o-2"
	if _, _, err := e.emit(t, r, other, nil); err != nil {
		t.Fatalf("đối tượng khác cùng seq: %v", err)
	}
}

// Sự kiện sai dạng → ErrInvalidEvent, không ghi gì.
func TestEmitValidation(t *testing.T) {
	e := newEnv(t)
	r := events.NewRegistry()
	for name, mut := range map[string]func(*events.Event){
		"thiếu topic":  func(ev *events.Event) { ev.Topic = "" },
		"thiếu loại":   func(ev *events.Event) { ev.AggregateType = "" },
		"thiếu mã":     func(ev *events.Event) { ev.AggregateID = "" },
		"seq 0":        func(ev *events.Event) { ev.Seq = 0 },
		"payload hỏng": func(ev *events.Event) { ev.Payload = func() {} },
	} {
		ev := e.ev(1, "order.delivered")
		mut(&ev)
		if _, _, err := e.emit(t, r, ev, nil); !errors.Is(err, events.ErrInvalidEvent) {
			t.Fatalf("%s: muốn ErrInvalidEvent, nhận %v", name, err)
		}
	}
	if n := e.countEvents(t, "true"); n != 0 {
		t.Fatalf("sự kiện %d", n)
	}
}

// Emit cho tenant khác tenant của transaction → RLS chặn (không ghi sự kiện sang shop khác).
func TestEmitOtherTenantRejectedByRLS(t *testing.T) {
	e := newEnv(t)
	other := e.Tenant(t, "b")
	r := events.NewRegistry()
	err := tenancy.WithTenant(t.Context(), e.App, other, func(tx pgx.Tx) error {
		_, _, err := r.Emit(t.Context(), tx, e.q, e.ev(1, "order.delivered"))
		return err //nolint:wrapcheck // test
	})
	if err == nil {
		t.Fatal("ghi sự kiện của tenant a trong ngữ cảnh tenant b phải bị chặn")
	}
}

// domain_events chỉ ghi thêm: crm_app không sửa, không xóa được.
func TestDomainEventsAppendOnly(t *testing.T) {
	e := newEnv(t)
	if _, _, err := e.emit(t, events.NewRegistry(), e.ev(1, "order.delivered"), nil); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"UPDATE platform.domain_events SET topic = 'x'", "DELETE FROM platform.domain_events"} {
		err := tenancy.WithTenant(t.Context(), e.App, e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), q)
			return err //nolint:wrapcheck // test
		})
		if err == nil {
			t.Fatalf("%s phải lỗi", q)
		}
	}
	// Chủ bảng (thấy dòng trong ngữ cảnh tenant) cũng không sửa, không xóa được: trigger chặn.
	for _, q := range []string{"UPDATE platform.domain_events SET topic = 'x'", "DELETE FROM platform.domain_events"} {
		_, err := e.Owner.Pool.Exec(t.Context(), "BEGIN; SELECT set_config('app.tenant_id', '"+e.tenant.String()+"', true); "+q+"; COMMIT")
		if err == nil || !strings.Contains(err.Error(), "chỉ ghi thêm") {
			t.Fatalf("%s bằng chủ bảng phải bị trigger chặn, nhận %v", q, err)
		}
	}
	if n := e.countEvents(t, "topic = 'order.delivered'"); n != 1 {
		t.Fatalf("sự kiện bị đổi: %d", n)
	}
}

// Register từ chối tên rỗng, tên trùng, tên sai mẫu, không topic.
func TestRegisterValidation(t *testing.T) {
	r := events.NewRegistry()
	if err := r.Register(newConsumer("aff", "order.delivered")); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]events.Consumer{
		"trùng":       newConsumer("aff", "order.returned"),
		"rỗng":        newConsumer("", "x"),
		"sai mẫu":     newConsumer("Aff:1", "x"),
		"không topic": newConsumer("other"),
	} {
		if err := r.Register(c); !errors.Is(err, events.ErrInvalidConsumer) {
			t.Fatalf("%s: muốn ErrInvalidConsumer, nhận %v", name, err)
		}
	}
	if got := len(r.JobHandlers()); got != 1 {
		t.Fatalf("JobHandlers %d, muốn 1", got)
	}
	if k := r.JobHandlers()[0].Kind(); k != events.ConsumerKind("aff") || k.Name != "event.aff" || !k.Policy.BlockOnDead {
		t.Fatalf("kind %+v", k)
	}
}
