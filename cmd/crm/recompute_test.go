package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// recomputeEnv: tenant A (trống) và tenant B có nhóm gốc X + hồ sơ con Y (Y gộp vào X), chưa đơn nào, với chỉ số bị sửa
// tay (mô phỏng số lệch khi không có việc lỗi) và nhãn theo quy tắc "order_count ≥ 1" đã gắn theo số lệch.
type recomputeEnv struct {
	s       *testkit.Staff
	a, b    uuid.UUID
	x, y    uuid.UUID
	tag     uuid.UUID
	created time.Time
}

func newRecomputeEnv(t *testing.T) *recomputeEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &recomputeEnv{s: s, a: s.Tenant(t, "A"), b: s.Tenant(t, "B"), created: time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)}
	w := customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &testkit.RecordingBus{}}
	actor := customer.Actor{Type: customer.ActorStaff, ID: uuid.New()}
	app := func(fn func(tx pgx.Tx) error) {
		t.Helper()
		if err := tenancy.WithTenant(t.Context(), s.App, e.b, fn); err != nil {
			t.Fatal(err)
		}
	}
	app(func(tx pgx.Tx) error {
		x, err := w.CreateCustomer(t.Context(), tx, customer.CreateInput{FullName: "Khách X"}, actor, e.created)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		y, err := w.CreateCustomer(t.Context(), tx, customer.CreateInput{FullName: "Khách Y"}, actor, e.created)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		e.x, e.y = x.ID, y.ID
		if _, err := w.MergeCustomers(t.Context(), tx, e.x, e.y, customer.MatchedByStaff, actor, e.created); err != nil {
			return err //nolint:wrapcheck // test
		}
		tag, err := w.CreateTag(t.Context(), tx, "Đã mua", "red", actor, e.created)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		e.tag = tag.ID
		_, err = w.PutTagRule(t.Context(), tx, tag.ID, customer.TagRuleInput{Enabled: true, Conditions: []customer.TagCondition{
			{Field: customer.FieldOrderCount, Op: customer.OpGte, Value: 1},
		}}, actor, e.created)
		return err //nolint:wrapcheck // test
	})
	s.Exec(t, e.b, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE customer.customers SET order_count = 5, delivered_count = 3,
			returned_count = 1, total_spent = 999000, last_order_at = $2 WHERE id = $1`, e.x, e.created)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = tx.Exec(t.Context(), `UPDATE customer.customers SET order_count = 2, total_spent = 1 WHERE id = $1`, e.y)
		return err //nolint:wrapcheck // test
	})
	app(func(tx pgx.Tx) error { return w.ApplyTagRules(t.Context(), tx, e.x, e.created) })
	if src := e.link(t); src != customer.TagSourceRule {
		t.Fatalf("trước lệnh: nhãn theo quy tắc trên X phải có (số lệch), có %q", src)
	}
	return e
}

// statsSum trả tổng mọi cột chỉ số (số đơn, đã giao, hoàn, tổng chi) của các hồ sơ ids; 0 nghĩa là mọi số đã về 0.
func (e *recomputeEnv) statsSum(t *testing.T, ids ...uuid.UUID) int64 {
	t.Helper()
	var sum int64
	e.s.Exec(t, e.b, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT coalesce(sum(order_count + delivered_count + returned_count + total_spent), 0)
			FROM customer.customers WHERE id = ANY($1)`, ids).Scan(&sum)
	})
	return sum
}

// link trả nguồn của dòng nhãn e.tag trên X ("" khi không có).
func (e *recomputeEnv) link(t *testing.T) string {
	t.Helper()
	var src string
	e.s.Exec(t, e.b, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT coalesce(max(source), '') FROM customer.customer_tag_links
			WHERE customer_id = $1 AND tag_id = $2`, e.x, e.tag).Scan(&src)
	})
	return src
}

// Task 434: lệnh tính lại chỉ số cho gốc hiện tại của khách (truyền mã hồ sơ con Y) — tìm đúng tenant B (bỏ qua tenant
// A), tính lại từ đơn, phiếu (không có → mọi số 0, cả gốc X và hồ sơ con Y), áp quy tắc nhãn (gỡ nhãn gắn theo số lệch),
// báo hiệu customer.changed cho gốc; chạy lần hai cho cùng kết quả. Mã không có ở tenant nào → errCustomerUnknown.
func TestRecomputeCustomerStats(t *testing.T) {
	e := newRecomputeEnv(t)
	bus := &testkit.RecordingBus{}
	for i := range 2 {
		res, err := recomputeCustomerStats(t.Context(), e.s.App, bus, e.y, e.created.Add(time.Hour))
		if err != nil {
			t.Fatalf("lần %d: %v", i+1, err)
		}
		if res.TenantID != e.b || res.RootID != e.x || res.Stats != (customer.Stats{}) {
			t.Errorf("lần %d: %+v, mong tenant %s, gốc %s, chỉ số 0", i+1, res, e.b, e.x)
		}
		if sum := e.statsSum(t, e.x, e.y); sum != 0 {
			t.Errorf("lần %d: tổng chỉ số của gốc X và hồ sơ con Y là %d, mong 0", i+1, sum)
		}
		if src := e.link(t); src != "" {
			t.Errorf("lần %d: nhãn theo quy tắc còn trên X (%q)", i+1, src)
		}
	}
	sigs := bus.Signals()
	if len(sigs) == 0 || sigs[0].Topic != "customer.changed" || sigs[0].TenantID != e.b || sigs[0].Entity.ID != e.x.String() {
		t.Errorf("báo hiệu: %+v, mong customer.changed của gốc X ở tenant B", sigs)
	}

	if _, err := recomputeCustomerStats(t.Context(), e.s.App, bus, uuid.New(), e.created); !errors.Is(err, errCustomerUnknown) {
		t.Errorf("khách không có: %v, mong errCustomerUnknown", err)
	}
}

// recomputeEnvVars: cấu hình tối thiểu của vai trò migrate (DATABASE_OWNER_URL không được mở khi tính lại).
func recomputeEnvVars(dbURL string) func(string) string {
	vars := map[string]string{
		"CRM_ROLE": "migrate", "CRM_ENV": "test", "DATABASE_URL": dbURL,
		"DATABASE_OWNER_URL": "postgres://khong-dung@127.0.0.1:1/x", "OTEL_ENABLED": "false",
	}
	return func(k string) string { return vars[k] }
}

// `crm recompute-customer-stats <mã>` qua run: mã thoát 0 và một dòng kết quả có mã tenant, mã gốc, các số; khách không
// có → mã thoát 1.
func TestRunRecompute(t *testing.T) {
	e := newRecomputeEnv(t)
	getenv := recomputeEnvVars(e.s.App.Pool.Config().ConnString())
	var out, errOut bytes.Buffer
	if code := run(t.Context(), []string{recomputeCommand, e.y.String()}, getenv, &out, &errOut); code != exitOK {
		t.Fatalf("mã thoát %d, stderr %s", code, errOut.String())
	}
	for _, want := range []string{e.b.String(), e.x.String(), "0 đơn"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("kết quả thiếu %q: %s", want, out.String())
		}
	}
	if sum := e.statsSum(t, e.x, e.y); sum != 0 {
		t.Errorf("sau lệnh: tổng chỉ số của X, Y là %d", sum)
	}
	errOut.Reset()
	if code := run(t.Context(), []string{recomputeCommand, uuid.NewString()}, getenv, &out, &errOut); code != exitFail {
		t.Errorf("khách không có: mã thoát %d, mong %d (%s)", code, exitFail, errOut.String())
	}
}

// Thiếu mã hay mã sai dạng → mã thoát 2 (cấu hình), không mở database (DATABASE_URL trỏ vào cổng không có ai).
func TestRunRecomputeArgs(t *testing.T) {
	t.Parallel()
	getenv := recomputeEnvVars("postgres://crm_app@127.0.0.1:1/x")
	for name, args := range map[string][]string{
		"thiếu mã": {recomputeCommand}, "mã sai dạng": {recomputeCommand, "khong-phai-uuid"},
		"thừa đối số": {recomputeCommand, uuid.NewString(), "x"},
	} {
		var out, errOut bytes.Buffer
		if code := run(t.Context(), args, getenv, &out, &errOut); code != exitConfig {
			t.Errorf("%s: mã thoát %d, mong %d", name, code, exitConfig)
		}
		if !strings.Contains(errOut.String(), recomputeCommand) {
			t.Errorf("%s: stderr phải nêu cách gọi lệnh: %s", name, errOut.String())
		}
	}
}
