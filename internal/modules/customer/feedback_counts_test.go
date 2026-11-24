package customer_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

func (e *writeEnv) feedbackCounts(tenant uuid.UUID) customer.FeedbackCounts {
	e.t.Helper()
	var c customer.FeedbackCounts
	if err := e.try(tenant, func(tx pgx.Tx) error {
		var err error
		c, err = customer.CountFeedback(e.t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return c
}

// [Task 486] Số góp ý theo trạng thái (đủ ba trạng thái, kể cả 0) và tổng; tenant khác không tính. Góp ý gắn hồ sơ có
// mã KH của gốc hiện tại (sau khi gộp: mã của gốc).
func TestFeedback_CountsAndCustomerCode(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	if c := e.feedbackCounts(e.tenant); c.Total != 0 || len(c.ByState) != 3 || c.ByState[customer.FeedbackNew] != 0 {
		t.Fatalf("rỗng: %+v", c)
	}
	a := e.create(customer.CreateInput{FullName: "Chị Lan", Phone: "0912000111"})
	b := e.create(customer.CreateInput{FullName: "Lan Nguyễn", Phone: "0987000222"})
	e.mustMerge(a.ID, b.ID)
	byB := e.mustFeedback(customer.FeedbackInput{Topic: "delivery", Body: "Giao chậm", Contact: "0987000222"})
	e.mustFeedback(customer.FeedbackInput{Topic: "other", Body: "Khách lạ"})
	seen := e.mustFeedback(customer.FeedbackInput{Topic: "product", Body: "Áo đẹp"})
	if _, err := e.updateFeedback(e.tenant, seen, 1, new(customer.FeedbackSeen), nil); err != nil {
		t.Fatal(err)
	}
	c := e.feedbackCounts(e.tenant)
	if c.Total != 3 || c.ByState[customer.FeedbackNew] != 2 || c.ByState[customer.FeedbackSeen] != 1 ||
		c.ByState[customer.FeedbackAnswered] != 0 {
		t.Fatalf("đếm: %+v", c)
	}
	if o := e.feedbackCounts(e.other); o.Total != 0 {
		t.Fatalf("tenant B: %+v", o)
	}
	rows, _, err := e.listFeedback(e.tenant, customer.FeedbackFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rows {
		switch {
		case f.ID == byB:
			if f.CustomerCode != a.Code || !khPattern.MatchString(f.CustomerCode) {
				t.Errorf("mã KH: %q, mong %q", f.CustomerCode, a.Code)
			}
		case f.CustomerCode != "":
			t.Errorf("góp ý không gắn có mã KH %q", f.CustomerCode)
		}
	}
}

// feedbackPlanSeed: tenant A 2.000 góp ý, tenant B 20.000 (rải ba trạng thái); ANALYZE.
func feedbackPlanSeed(a, b uuid.UUID) testkit.Seed {
	rows := func(tenant uuid.UUID, n int) string {
		return fmt.Sprintf(`SELECT set_config('app.tenant_id', '%s', false);
			INSERT INTO customer.feedback (tenant_id, topic, body, state, created_at)
			SELECT current_setting('app.tenant_id')::uuid, 'other', 'góp ý ' || g, (ARRAY['new','seen','answered'])[1 + g %% 3],
			       timestamptz '2026-01-01' + g * interval '1 minute'
			FROM generate_series(1, %d) AS g`, tenant, n)
	}
	return testkit.Seed{Name: "customer-feedback-plan", SQL: []string{
		"SET ROLE crm_owner",
		fmt.Sprintf("INSERT INTO platform.tenants (id, name) VALUES ('%s', 'A'), ('%s', 'B')", a, b),
		rows(a, 2000), rows(b, 20000), "ANALYZE customer.feedback",
	}}
}

// [Task 486] Câu đếm góp ý theo trạng thái (câu sqlc countFeedback) bằng crm_app trên tenant A (2.000 dòng, bảng có
// 22.000): không Seq Scan (index feedback_state_created); CountFeedback đếm đúng.
func TestCountFeedback_Plan(t *testing.T) {
	t.Parallel()
	a, b := testkit.FixedUUID("customer-feedback-plan/A"), testkit.FixedUUID("customer-feedback-plan/B")
	staff := testkit.OpenStaffSeeded(t, feedbackPlanSeed(a, b))
	var c customer.FeedbackCounts
	if err := tenancy.WithTenant(t.Context(), staff.App, a, func(tx pgx.Tx) error {
		var err error
		c, err = customer.CountFeedback(t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if c.Total != 2000 || c.ByState[customer.FeedbackSeen] != 667 {
		t.Fatalf("đếm: %+v", c)
	}
	plan, err := testkit.ExplainAsApp(t.Context(), staff.App, a, storeSQL(t, "countFeedback"))
	if err != nil {
		t.Fatal(err)
	}
	testkit.RequireNoSeqScan(t, plan)
}
