package automation_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// greetingConfig là cấu hình tin chào hợp lệ (quy tắc gửi tin: cổng không liên hệ áp dụng).
const greetingConfig = `{"kind":"greeting","text":"Dạ shop chào chị","quickReplies":[]}`

// saveRule lưu quy tắc kind (bật) với cấu hình cfg, trả quy tắc đã lưu.
func (e *env) saveRule(kind, cfg string) automation.Rule {
	e.t.Helper()
	var r automation.Rule
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		var err error
		_, r, err = automation.SaveRule(e.t.Context(), tx, e.page, kind, true, json.RawMessage(cfg), 0, e.user)
		return err //nolint:wrapcheck // test
	})
	return r
}

// customer tạo một khách (chủ bảng), trả mã.
func (e *env) customer() uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `INSERT INTO customer.customers (tenant_id, full_name, name_search)
			VALUES (current_setting('app.tenant_id')::uuid, 'Chị Lan', 'chi lan') RETURNING id`).Scan(&id)
	})
	return id
}

// runOnce chạy Run một lần trong tx tenant với act; acts đếm số lần act được gọi.
func (e *env) runOnce(rule automation.Rule, t automation.Trigger, act func(context.Context, pgx.Tx) (uuid.UUID, error)) (automation.Outcome, error) {
	e.t.Helper()
	var out automation.Outcome
	err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		acc, err := channel.Account(e.t.Context(), tx, e.page)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		t.Account = acc
		out, err = automation.Run(e.t.Context(), tx, e.d, rule, t, act)
		return err //nolint:wrapcheck // test
	})
	return out, err //nolint:wrapcheck // test
}

func (e *env) runRows(ref string) (outcome string, n int) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT coalesce(max(outcome), ''), count(*) FROM conversation.automation_runs
			WHERE external_ref = $1`, ref).Scan(&outcome, &n)
	})
	return outcome, n
}

// gateEnv: env có quy tắc greeting (gửi tin) và auto_hide (all) đã bật, một khách, và act đếm số lần được gọi.
type gateEnv struct {
	*env
	greet, hide automation.Rule
	cust        uuid.UUID
	acts        int
}

func newGateEnv(t *testing.T) *gateEnv {
	t.Helper()
	g := &gateEnv{env: newEnv(t)}
	g.greet = g.saveRule(automation.KindGreeting, greetingConfig)
	g.hide = g.saveRule(automation.KindAutoHide, `{"kind":"auto_hide","mode":"all","exceptPostIds":[]}`)
	g.cust = g.customer()
	return g
}

func (g *gateEnv) act(context.Context, pgx.Tx) (uuid.UUID, error) {
	g.acts++
	return uuid.Nil, nil
}

// trigger là sự kiện mới (vừa xảy ra) của khách g.cust với khóa ref.
func (g *gateEnv) trigger(ref string) automation.Trigger {
	return automation.Trigger{CustomerID: g.cust, OccurredAt: time.Now().UTC(), SubjectRef: "s", ExternalRef: ref}
}

// Cổng đầu của Run: quy tắc tắt, sự kiện của chính Page, tin lịch sử → OutcomeNone, không ghi dòng, act không chạy.
func TestRun_NoRowGates(t *testing.T) {
	t.Parallel()
	g := newGateEnv(t)
	off := g.greet
	off.Enabled = false
	page, hist := g.trigger("r-page"), g.trigger("r-hist")
	page.FromPage, hist.IsHistory = true, true
	for name, c := range map[string]struct {
		rule automation.Rule
		tr   automation.Trigger
	}{"tắt": {off, g.trigger("r-off")}, "của Page": {g.greet, page}, "lịch sử": {g.greet, hist}} {
		if out, err := g.runOnce(c.rule, c.tr, g.act); err != nil || out != automation.OutcomeNone {
			t.Fatalf("%s: %q %v", name, out, err)
		}
		if _, n := g.runRows(c.tr.ExternalRef); n != 0 {
			t.Fatalf("%s: %d dòng", name, n)
		}
	}
	if g.acts != 0 {
		t.Fatalf("act chạy %d lần", g.acts)
	}
}

// Khách không liên hệ → skipped_dnc với quy tắc gửi tin, nhưng không chặn auto_hide; lần hai cùng ExternalRef →
// already_run, act không chạy lại.
func TestRun_DoNotContactAndOnce(t *testing.T) {
	t.Parallel()
	g := newGateEnv(t)
	g.staff.Exec(t, g.tenant, func(tx pgx.Tx) error {
		w := customer.Writer{Events: g.d.Events, Queue: g.d.Queue}
		return w.SetDoNotContact(t.Context(), tx, g.cust, true, customer.Actor{Type: customer.ActorSystem}, time.Now())
	})
	if out, err := g.runOnce(g.greet, g.trigger("r-dnc"), g.act); err != nil || out != automation.OutcomeSkippedDNC {
		t.Fatalf("không liên hệ: %q %v", out, err)
	}
	if out, err := g.runOnce(g.hide, g.trigger("r-hide"), g.act); err != nil || out != automation.OutcomeEnqueued || g.acts != 1 {
		t.Fatalf("ẩn với khách không liên hệ: %q %v, act %d", out, err, g.acts)
	}
	if out, err := g.runOnce(g.hide, g.trigger("r-hide"), g.act); err != nil || out != automation.OutcomeAlreadyRun || g.acts != 1 {
		t.Fatalf("chạy lại: %q %v, act %d", out, err, g.acts)
	}
}

// act trả Skip(limit) → lượt skipped_limit (một dòng, không lệnh); act lỗi → lỗi trả lên, tx lùi, không dòng.
func TestRun_ActSkipAndError(t *testing.T) {
	t.Parallel()
	g := newGateEnv(t)
	limit := func(context.Context, pgx.Tx) (uuid.UUID, error) {
		return uuid.Nil, automation.Skip(automation.OutcomeSkippedLimit)
	}
	if out, err := g.runOnce(g.hide, g.trigger("r-limit"), limit); err != nil || out != automation.OutcomeSkippedLimit {
		t.Fatalf("giới hạn: %q %v", out, err)
	}
	if o, n := g.runRows("r-limit"); o != string(automation.OutcomeSkippedLimit) || n != 1 {
		t.Fatalf("giới hạn ghi %q, %d dòng", o, n)
	}
	boom := errors.New("lỗi ghi lệnh")
	fail := func(context.Context, pgx.Tx) (uuid.UUID, error) { return uuid.Nil, boom }
	if _, err := g.runOnce(g.hide, g.trigger("r-fail"), fail); !errors.Is(err, boom) {
		t.Fatalf("act lỗi: %v", err)
	}
	if _, n := g.runRows("r-fail"); n != 0 {
		t.Fatalf("act lỗi vẫn còn %d dòng", n)
	}
}

// View công bố automation_rules_v: security_invoker, đúng cột của docs/views.md (không có config); lưu lần đầu một loại
// → view có đúng một dòng (Page, loại); lưu lần hai (version 1) vẫn một dòng, version 2; version cũ → xung đột;
// cấu hình sai (chế độ lạ, kind khác loại) → ErrInvalidRule; lược đồ đúng luật tenant.
func TestAutomationRulesView(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	testkit.CheckSchemaRulesClean(t, e.staff.Owner)
	testkit.CheckViews(t, e.staff.Owner, "conversation", map[string]string{
		"automation_rules_v": "tenant_id, id, channel_account_id, kind, enabled, updated_at, version",
	})
	count := func() (n int) {
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `SELECT count(*) FROM conversation.automation_rules_v
				WHERE channel_account_id = $1 AND kind = 'auto_hide'`, e.page).Scan(&n)
		})
		return n
	}
	if count() != 0 {
		t.Fatal("view có dòng trước khi lưu")
	}
	e.setRule(true, automation.HidePhoneOnly)
	e.setRule(false, automation.HideAll)
	if count() != 1 {
		t.Fatalf("view có %d dòng, muốn 1", count())
	}
	save := func(cfg string, version int32) error {
		return tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
			_, _, err := automation.SaveRule(t.Context(), tx, e.page, automation.KindAutoHide, true, json.RawMessage(cfg), version, e.user)
			return err //nolint:wrapcheck // test
		})
	}
	ok := `{"kind":"auto_hide","mode":"all","exceptPostIds":[]}`
	if err := save(ok, 1); !errors.Is(err, automation.ErrRuleVersionConflict) {
		t.Fatalf("version cũ: %v", err)
	}
	for _, bad := range []string{
		`{"kind":"auto_hide","mode":"abc","exceptPostIds":[]}`, `{"kind":"greeting","text":"x","quickReplies":[]}`, `[]`,
	} {
		if err := save(bad, 2); !errors.Is(err, automation.ErrInvalidRule) {
			t.Fatalf("cấu hình %s: %v", bad, err)
		}
	}
	var rules []automation.Rule
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) (err error) {
		rules, err = automation.ListRules(t.Context(), tx, e.page)
		return err //nolint:wrapcheck // test
	})
	if len(rules) != 4 || rules[0].Version != 2 || rules[0].Enabled || rules[1].Version != 0 || rules[1].Kind != automation.KindAutoReplyComment {
		t.Fatalf("danh sách: %+v", rules)
	}
}
