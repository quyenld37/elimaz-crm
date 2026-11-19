package customer_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

func errorsIsPermanent(err error) bool { return errors.Is(err, queue.ErrPermanent) }

func (e *statsEnv) tag(name string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.app(func(tx pgx.Tx) error {
		t, err := e.cw.CreateTag(e.t.Context(), tx, name, "red", e.actor, e.now)
		id = t.ID
		return err //nolint:wrapcheck // test
	})
	return id
}

func (e *statsEnv) putRule(tagID uuid.UUID, version *int64, enabled bool, conds ...customer.TagCondition,
) (customer.TagRule, error) {
	var r customer.TagRule
	err := e.try(func(tx pgx.Tx) error {
		var err error
		r, err = e.cw.PutTagRule(e.t.Context(), tx, tagID, customer.TagRuleInput{
			Version: version, Enabled: enabled, Conditions: conds,
		}, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	return r, err
}

func (e *statsEnv) try(fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

// links trả nguồn của dòng nhãn tagID trên hồ sơ c ("" khi không có).
func (e *statsEnv) link(c, tagID uuid.UUID) string {
	e.t.Helper()
	var src string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		err := tx.QueryRow(e.t.Context(), `SELECT coalesce(max(source), '') FROM customer.customer_tag_links
			WHERE customer_id = $1 AND tag_id = $2`, c, tagID).Scan(&src)
		return err //nolint:wrapcheck // test
	})
	return src
}

func cond(field, op string, v int64) customer.TagCondition {
	return customer.TagCondition{Field: field, Op: op, Value: v}
}

// Kế hoạch M2-25, ca 4: quy tắc "Hay hoàn" (return_rate_bp gte 3000 VÀ order_count gte 2): A (1 giao, 1 hoàn) được gắn
// source rule; khách C có nhãn cùng tên gắn tay, không đạt → dòng staff còn nguyên; A thêm hai đơn giao xong → tỷ lệ
// 2500 → nhãn được gỡ. Tắt quy tắc → dòng rule bị gỡ khi áp lại.
func TestTagRuleReturnRate(t *testing.T) {
	e := newStatsEnv(t)
	tag := e.tag("Hay hoàn")
	r, err := e.putRule(tag, nil, true, cond(customer.FieldReturnRateBP, customer.OpGte, 3000),
		cond(customer.FieldOrderCount, customer.OpGte, 2))
	if err != nil || r.Version != 1 || !r.Enabled || len(r.Conditions) != 2 || r.TagID != tag {
		t.Fatalf("đặt quy tắc: %+v %v", r, err)
	}
	a, c := e.newCustomer("Khách A"), e.newCustomer("Khách C")
	e.app(func(tx pgx.Tx) error {
		return e.cw.AddTag(t.Context(), tx, c, tag, customer.TagSourceStaff, e.actor, e.now)
	})
	e.delivered(a, 100000)
	e.carrierReturned(a, 100000)
	e.delivered(c, 100000) // C: tỷ lệ 0, sự kiện của C áp quy tắc cho C
	e.feed(false)
	if got := e.link(a, tag); got != customer.TagSourceRule {
		t.Fatalf("A phải có nhãn rule: %q", got)
	}
	if got := e.link(c, tag); got != customer.TagSourceStaff {
		t.Fatalf("nhãn gắn tay của C: %q", got)
	}
	e.delivered(a, 100000)
	e.delivered(a, 100000)
	e.feed(false)
	if s := e.stats(a); s.ReturnRateBP != 2500 {
		t.Fatalf("tỷ lệ hoàn sau hai đơn: %d", s.ReturnRateBP)
	}
	if got := e.link(a, tag); got != "" {
		t.Fatalf("nhãn rule của A phải bị gỡ: %q", got)
	}
	if got := e.link(c, tag); got != customer.TagSourceStaff {
		t.Fatalf("nhãn gắn tay của C bị đụng: %q", got)
	}

	// Đổi quy tắc cho A đạt lại, rồi tắt: dòng rule gỡ khi áp lại; version lệch → ErrVersionConflict.
	v := r.Version
	r, err = e.putRule(tag, &v, true, cond(customer.FieldReturnRateBP, customer.OpGte, 2000))
	if err != nil || r.Version != 2 {
		t.Fatalf("sửa quy tắc: %+v %v", r, err)
	}
	e.app(func(tx pgx.Tx) error { return e.cw.ApplyTagRules(t.Context(), tx, a, e.now) })
	if got := e.link(a, tag); got != customer.TagSourceRule {
		t.Fatalf("A đạt quy tắc mới: %q", got)
	}
	if _, err := e.putRule(tag, &v, false, cond(customer.FieldReturnRateBP, customer.OpGte, 2000)); !errors.Is(err,
		customer.ErrVersionConflict) {
		t.Fatalf("version cũ: %v", err)
	}
	if _, err := e.putRule(tag, nil, false, cond(customer.FieldReturnRateBP, customer.OpGte, 2000)); !errors.Is(err,
		customer.ErrVersionConflict) {
		t.Fatalf("thiếu version khi đã có quy tắc: %v", err)
	}
	v = r.Version
	if _, err := e.putRule(tag, &v, false, cond(customer.FieldReturnRateBP, customer.OpGte, 2000)); err != nil {
		t.Fatal(err)
	}
	e.app(func(tx pgx.Tx) error { return e.cw.ApplyTagRules(t.Context(), tx, a, e.now) })
	if got := e.link(a, tag); got != "" {
		t.Fatalf("quy tắc tắt, dòng rule phải gỡ: %q", got)
	}
	var got customer.TagRule
	e.app(func(tx pgx.Tx) error {
		var err error
		got, err = customer.GetTagRule(t.Context(), tx, tag)
		return err //nolint:wrapcheck // test
	})
	if got.Enabled || got.Version != 3 || len(got.Conditions) != 1 || got.Conditions[0].Value != 2000 {
		t.Fatalf("đọc quy tắc: %+v", got)
	}
}

// Kế hoạch M2-25, ca 5: "Lâu không mua" days_since_last_order gte 90: lịch đêm ở giờ +89 ngày không gắn, +91 ngày gắn
// (source rule); khách chưa mua đơn nào không được gắn. Lô 1 gốc: việc nối tiếp có trong hàng đợi.
func TestTagRuleNightly(t *testing.T) {
	e := newStatsEnv(t)
	tag := e.tag("Lâu không mua")
	if _, err := e.putRule(tag, nil, true, cond(customer.FieldDaysSinceLastOrder, customer.OpGte, 90)); err != nil {
		t.Fatal(err)
	}
	a, never := e.newCustomer("Khách A"), e.newCustomer("Chưa mua")
	e.delivered(a, 100000)
	e.feed(false)
	run := func(at time.Time) {
		e.t.Helper()
		h := customer.NightlyHandlerForTest(e.cw, func() time.Time { return at }, 1000)
		e.app(func(tx pgx.Tx) error {
			_, err := h.Handle(t.Context(), queue.Job{ID: uuid.New(), TenantID: &e.tenant, Payload: json.RawMessage(`{}`)}, tx)
			return err //nolint:wrapcheck // test
		})
	}
	run(e.now.Add(89 * 24 * time.Hour))
	if got := e.link(a, tag); got != "" {
		t.Fatalf("+89 ngày: %q", got)
	}
	run(e.now.Add(91 * 24 * time.Hour))
	if got := e.link(a, tag); got != customer.TagSourceRule {
		t.Fatalf("+91 ngày: %q", got)
	}
	if got := e.link(never, tag); got != "" {
		t.Fatalf("khách chưa mua: %q", got)
	}

	// Lô 1 gốc: việc đầu xử lý một gốc rồi ghi việc nối tiếp (payload after = gốc vừa xử lý).
	h := customer.NightlyHandlerForTest(e.cw, func() time.Time { return e.now }, 1)
	e.app(func(tx pgx.Tx) error {
		_, err := h.Handle(t.Context(), queue.Job{
			ID: uuid.New(), TenantID: &e.tenant,
			Payload: json.RawMessage(`{"slot":"2026-11-20T00:00:00Z"}`),
		}, tx)
		return err //nolint:wrapcheck // test
	})
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = $1 AND payload ? 'after'`,
		"task."+customer.TagRulesNightlySchedule); n != 1 {
		t.Fatalf("việc nối tiếp: %d", n)
	}
}

// Kế hoạch M2-25, ca 6: trường lạ (phone), phép lạ, không điều kiện, quá 10 điều kiện, giá trị âm → ErrInvalidTagRule;
// nhãn không có → ErrNotFound; quy tắc chưa có → GetTagRule ErrNotFound.
func TestTagRuleInvalid(t *testing.T) {
	e := newStatsEnv(t)
	tag := e.tag("VIP")
	many := make([]customer.TagCondition, 11)
	for i := range many {
		many[i] = cond(customer.FieldOrderCount, customer.OpGte, 1)
	}
	for name, cs := range map[string][]customer.TagCondition{
		"phone":    {cond("phone", customer.OpGte, 1)},
		"phép lạ":  {cond(customer.FieldOrderCount, "eq", 1)},
		"rỗng":     {},
		"11":       many,
		"âm":       {cond(customer.FieldTotalSpent, customer.OpGte, -1)},
		"quá 2^53": {cond(customer.FieldTotalSpent, customer.OpLte, 1<<53)},
	} {
		if _, err := e.putRule(tag, nil, true, cs...); !errors.Is(err, customer.ErrInvalidTagRule) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.putRule(uuid.New(), nil, true, cond(customer.FieldOrderCount, customer.OpGte, 1)); !errors.Is(err,
		customer.ErrNotFound) {
		t.Errorf("nhãn lạ: %v", err)
	}
	err := e.try(func(tx pgx.Tx) error {
		_, err := customer.GetTagRule(t.Context(), tx, tag)
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("quy tắc chưa có: %v", err)
	}
}
