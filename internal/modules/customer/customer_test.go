package customer_test

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

var khPattern = regexp.MustCompile(`^KH[0-9]{6,}$`)

// writeEnv: database đã migrate, danh mục đơn vị mẫu (T01, T02 …), tenant A và B, một Writer có bus ghi báo hiệu.
type writeEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
	other  uuid.UUID
	bus    *realtimetest.RecordingBus
	w      customer.Writer
	actor  customer.Actor
	now    time.Time
}

func newWriteEnv(t *testing.T) *writeEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	seedAdminUnits(t, s)
	e := &writeEnv{
		t: t, staff: s, tenant: s.Tenant(t, "A"), other: s.Tenant(t, "B"), bus: &realtimetest.RecordingBus{},
		actor: customer.Actor{Type: customer.ActorStaff, ID: uuid.New(), RequestID: "req-1", IP: "10.0.0.1"},
		now:   time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC),
	}
	e.w = customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: e.bus}
	return e
}

// inTx chạy fn bằng crm_app trong tenant A; lỗi làm test đỏ.
func (e *writeEnv) inTx(fn func(tx pgx.Tx) error) {
	e.t.Helper()
	if err := e.try(e.tenant, fn); err != nil {
		e.t.Fatal(err)
	}
}

// try chạy fn bằng crm_app trong tenant và trả lỗi.
func (e *writeEnv) try(tenant uuid.UUID, fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, tenant, fn) //nolint:wrapcheck // test
}

func (e *writeEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

// create tạo khách bằng Writer trong tenant A (đòi thành công).
// pair tạo hai khách tên nameA (không SĐT) và nameB (SĐT phoneB, rỗng là không có).
func (e *writeEnv) pair(nameA, nameB, phoneB string) (a, b customer.Customer) {
	e.t.Helper()
	return e.create(customer.CreateInput{FullName: nameA}), e.create(customer.CreateInput{FullName: nameB, Phone: phoneB})
}

func (e *writeEnv) create(in customer.CreateInput) customer.Customer {
	e.t.Helper()
	var c customer.Customer
	e.inTx(func(tx pgx.Tx) error {
		var err error
		c, err = e.w.CreateCustomer(e.t.Context(), tx, in, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	return c
}

func (e *writeEnv) get(id uuid.UUID) customer.Profile {
	e.t.Helper()
	var p customer.Profile
	e.inTx(func(tx pgx.Tx) error {
		var err error
		p, err = customer.Get(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	})
	return p
}

// Lớp bọc M1 (Q30): customer.Create(ctx, tx, "Lan") trả id; hồ sơ có code KH…, root_id = id, name_search "lan",
// sự kiện customer.created (Seq 1), audit của người làm system; tên rỗng vẫn ErrInvalidInput như M1.
func TestCreateM1Wrapper(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	var id uuid.UUID
	e.inTx(func(tx pgx.Tx) error {
		var err error
		id, err = customer.Create(t.Context(), tx, "  Lan ")
		return err //nolint:wrapcheck // test
	})
	var root uuid.UUID
	var code, full, search, lifecycle string
	e.inTx(func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT root_id, code, full_name, name_search, lifecycle FROM customer.customers
			WHERE id = $1`, id).Scan(&root, &code, &full, &search, &lifecycle)
	})
	if root != id || !khPattern.MatchString(code) || full != "Lan" || search != "lan" || lifecycle != "lead" {
		t.Fatalf("khách của lớp bọc: root %s id %s code %q tên %q search %q lifecycle %q", root, id, code, full, search, lifecycle)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'customer.created'
		AND aggregate_type = 'customer' AND aggregate_id = $1 AND seq = 1`, id.String()); n != 1 {
		t.Errorf("sự kiện customer.created: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.create' AND entity_id = $1
		AND actor_type = 'system' AND actor_id IS NULL`, id.String()); n != 1 {
		t.Errorf("audit customer.create của system: %d", n)
	}
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		_, err := customer.Create(t.Context(), tx, "  ")
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, customer.ErrInvalidInput) {
		t.Fatalf("tên rỗng: %v", err)
	}
}
