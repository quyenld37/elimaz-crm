package queue_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
)

var errRollback = errors.New("bên gọi lùi transaction")

func TestEnqueueRolledBackLeavesNoRow(t *testing.T) {
	e := newEnv(t)
	err := e.app.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
		if _, _, err := e.q.Enqueue(t.Context(), tx, kind("t.rollback"), queue.NewJob{DedupeKey: "k"}); err != nil {
			return err //nolint:wrapcheck // test
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("muốn errRollback, nhận %v", err)
	}
	if n := e.count(t, "SELECT count(*) FROM platform.jobs"); n != 0 {
		t.Fatalf("transaction lùi mà còn %d việc", n)
	}
}

func TestEnqueueDedupe(t *testing.T) {
	e := newEnv(t)
	a, b := e.tenant(t, "A"), e.tenant(t, "B")
	k := kind("t.dedupe")

	id1, c1 := e.tryEnqueue(t, k, queue.NewJob{TenantID: &a, DedupeKey: "same", Payload: map[string]string{"id": "1"}})
	id2, c2 := e.tryEnqueue(t, k, queue.NewJob{TenantID: &a, DedupeKey: "same", Payload: map[string]string{"id": "2"}})
	if !c1 || c2 || id1 != id2 {
		t.Fatalf("cùng tenant, cùng khóa: (%v, %v) (%v, %v); muốn lần hai created=false và trả id cũ", id1, c1, id2, c2)
	}
	if n := e.count(t, "SELECT count(*) FROM platform.jobs WHERE tenant_id = $1", a); n != 1 {
		t.Fatalf("tenant A có %d việc, muốn 1", n)
	}

	idB, cB := e.tryEnqueue(t, k, queue.NewJob{TenantID: &b, DedupeKey: "same"})
	if !cB || idB == id1 {
		t.Fatalf("tenant khác cùng khóa phải là việc mới: %v %v", idB, cB)
	}

	n1, cn1 := e.tryEnqueue(t, k, queue.NewJob{DedupeKey: "same"})
	n2, cn2 := e.tryEnqueue(t, k, queue.NewJob{DedupeKey: "same"})
	if !cn1 || cn2 || n1 != n2 {
		t.Fatalf("không tenant hai lần: (%v, %v) (%v, %v); muốn 1 dòng (NULLS NOT DISTINCT)", n1, cn1, n2, cn2)
	}
	if n := e.count(t, "SELECT count(*) FROM platform.jobs"); n != 3 {
		t.Fatalf("muốn 3 dòng (A, B, không tenant), có %d", n)
	}

	// Khác kind, cùng khóa: việc khác.
	if _, c := e.tryEnqueue(t, kind("t.other"), queue.NewJob{TenantID: &a, DedupeKey: "same"}); !c {
		t.Fatal("khác kind cùng khóa phải là việc mới")
	}
}

func TestEnqueueStoresPolicyAndPayload(t *testing.T) {
	e := newEnv(t)
	k := queue.Kind{Name: "t.policy", Policy: queue.Policy{
		Mode: "transactional", MaxAttempts: 3, BlockOnDead: true, OnLeaseExpired: "in_doubt",
	}}
	id := e.enqueue(t, k, queue.NewJob{DedupeKey: "k", OrderKey: "o1", OrderSeq: 7, Payload: map[string]int{"order": 42}})
	var (
		mode, onLease, payload, orderKey string
		maxAttempts                      int
		block, hasExpiry                 bool
		seq                              int64
	)
	if err := e.owner.Pool.QueryRow(t.Context(), `SELECT mode, on_lease_expired, payload::text, order_key, order_seq,
		max_attempts, block_on_dead, expires_at IS NOT NULL FROM platform.jobs WHERE id = $1`, id).
		Scan(&mode, &onLease, &payload, &orderKey, &seq, &maxAttempts, &block, &hasExpiry); err != nil {
		t.Fatal(err)
	}
	if mode != "transactional" || onLease != "in_doubt" || payload != `{"order": 42}` || orderKey != "o1" || seq != 7 ||
		maxAttempts != 3 || !block || hasExpiry {
		t.Fatalf("dòng ghi sai: %s %s %s %s %d %d %v %v", mode, onLease, payload, orderKey, seq, maxAttempts, block, hasExpiry)
	}
}

// Policy để trống lấy DefaultPolicy; khóa chống trùng rỗng hay tên loại rỗng là lỗi của bên gọi.
func TestEnqueueDefaultsAndValidation(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(t, queue.Kind{Name: "t.zero"}, queue.NewJob{DedupeKey: "k"})
	var (
		mode, onLease string
		maxAttempts   int
	)
	if err := e.owner.Pool.QueryRow(t.Context(), "SELECT mode, on_lease_expired, max_attempts FROM platform.jobs WHERE id = $1", id).
		Scan(&mode, &onLease, &maxAttempts); err != nil {
		t.Fatal(err)
	}
	if mode != "transactional" || onLease != "retry" || maxAttempts != 8 {
		t.Fatalf("Policy rỗng phải lấy DefaultPolicy: %s %s %d", mode, onLease, maxAttempts)
	}
	bad := []struct {
		k queue.Kind
		j queue.NewJob
	}{
		{kind(""), queue.NewJob{DedupeKey: "k"}},
		{kind("t.bad"), queue.NewJob{}},
		{queue.Kind{Name: "t.bad", Policy: queue.Policy{Mode: "other"}}, queue.NewJob{DedupeKey: "k"}},
		{queue.Kind{Name: "t.bad", Policy: queue.Policy{OnLeaseExpired: "other"}}, queue.NewJob{DedupeKey: "k"}},
		{kind("t.bad"), queue.NewJob{DedupeKey: "k", Payload: func() {}}},
		{kind("t.bad"), queue.NewJob{DedupeKey: "k", TenantID: &uuid.Nil}},
	}
	for i, c := range bad {
		err := e.app.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
			_, _, err := e.q.Enqueue(t.Context(), tx, c.k, c.j)
			return err //nolint:wrapcheck // test
		})
		if !errors.Is(err, queue.ErrInvalidJob) {
			t.Errorf("ca %d: muốn ErrInvalidJob, nhận %v", i, err)
		}
	}
}

// Ghi việc phát NOTIFY trên kênh jobs với tên loại (đánh thức worker), chỉ khi transaction commit.
func TestEnqueueNotifies(t *testing.T) {
	e := newEnv(t)
	conn, err := e.app.Pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(t.Context(), "LISTEN jobs"); err != nil {
		t.Fatal(err)
	}
	e.enqueue(t, kind("t.notify"), queue.NewJob{DedupeKey: "k"})
	n, err := conn.Conn().WaitForNotification(t.Context())
	if err != nil || n.Channel != "jobs" || n.Payload != "t.notify" {
		t.Fatalf("muốn NOTIFY jobs t.notify, nhận %+v, %v", n, err)
	}
}
