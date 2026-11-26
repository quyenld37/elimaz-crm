package inbound_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// assigneeOf đọc người phụ trách của hội thoại inbox PSID user trên Page 1 (uuid.Nil khi chưa ai nhận).
func (e *env) assigneeOf(user string) uuid.UUID {
	e.t.Helper()
	var a *uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT assignee_id FROM conversation.conversations
			WHERE channel_account_id = $1 AND kind = 'inbox' AND external_thread_ref = $2`, e.page1, user).Scan(&a)
	})
	if a == nil {
		return uuid.Nil
	}
	return *a
}

// [Task 159, ghi chú điều phối M4] Xoay vòng (Task 160) nối vào đường tin vào: Page 1 bật xoay vòng [u1, u2] → hội
// thoại mới của khách 1 giao u1, của khách 2 giao u2, của khách 3 lại u1; tin thứ hai của khách 1 không đổi người phụ
// trách; mỗi lần giao một sự kiện conversation.assigned lý do round_robin. Page chưa bật (không có dòng cài đặt): hội
// thoại mới không ai nhận (ca Page 1 trước khi bật).
func TestInbound_RoundRobinOnNewConversation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.receive("psid-rr-0", "mid-rr-0", "chào", 0)
	if got := e.assigneeOf("psid-rr-0"); got != uuid.Nil {
		t.Fatalf("Page chưa bật xoay vòng: người phụ trách = %s, muốn trống", got)
	}
	u1 := e.staff.User(t, e.tenant, "rr1@elimaz.test", "active")
	u2 := e.staff.User(t, e.tenant, "rr2@elimaz.test", "active")
	for _, u := range []uuid.UUID{u1, u2} {
		e.staff.Grant(t, e.tenant, u, []string{"conversation.read"}, "page", e.page1.String())
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO conversation.page_settings
			(tenant_id, channel_account_id, round_robin_enabled, round_robin_user_ids)
			VALUES ($1, $2, true, $3)`, e.tenant, e.page1, []uuid.UUID{u1, u2})
		return err //nolint:wrapcheck // test
	})
	e.receive("psid-rr-1", "mid-rr-1", "a", 10)
	e.receive("psid-rr-2", "mid-rr-2", "b", 20)
	e.receive("psid-rr-1", "mid-rr-1b", "c", 30)
	e.receive("psid-rr-3", "mid-rr-3", "d", 40)
	for user, want := range map[string]uuid.UUID{"psid-rr-1": u1, "psid-rr-2": u2, "psid-rr-3": u1} {
		if got := e.assigneeOf(user); got != want {
			t.Errorf("%s: người phụ trách = %s, muốn %s", user, got, want)
		}
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.domain_events
		WHERE topic = 'conversation.assigned' AND payload->>'reason' = 'round_robin'`); n != 3 {
		t.Fatalf("sự kiện conversation.assigned = %d, muốn 3", n)
	}
}
