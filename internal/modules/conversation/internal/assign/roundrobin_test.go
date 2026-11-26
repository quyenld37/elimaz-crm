package assign_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/convtest"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
)

func TestNextInRotation(t *testing.T) {
	t.Parallel()
	u1, u2, u3 := uuid.New(), uuid.New(), uuid.New()
	users := []uuid.UUID{u1, u2, u3}
	all := func(uuid.UUID) bool { return true }
	not2 := func(u uuid.UUID) bool { return u != u2 }
	gone := uuid.New()
	for _, c := range []struct {
		name  string
		users []uuid.UUID
		last  *uuid.UUID
		ok    func(uuid.UUID) bool
		want  uuid.UUID
		found bool
	}{
		{"chưa ai", users, nil, all, u1, true},
		{"sau u1", users, &u1, all, u2, true},
		{"sau u3 vòng lại", users, &u3, all, u1, true},
		{"bỏ qua u2", users, &u1, not2, u3, true},
		{"người cuối không còn trong danh sách", users, &gone, all, u1, true},
		{"không ai đủ quyền", users, nil, func(uuid.UUID) bool { return false }, uuid.Nil, false},
		{"danh sách rỗng", nil, nil, all, uuid.Nil, false},
	} {
		got, found := assign.NextInRotation(c.users, c.last, c.ok)
		if got != c.want || found != c.found {
			t.Errorf("%s: %v %v, muốn %v %v", c.name, got, found, c.want, c.found)
		}
	}
}

// Xoay vòng [u1, u2, u3]: 6 hội thoại mới → u1, u2, u3, u1, u2, u3; khóa u2 → bỏ qua; hội thoại đã có người phụ trách
// → không đổi; tắt → không giao; chưa có dòng cài đặt → không giao.
func TestRoundRobin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	u := []uuid.UUID{
		convtest.Staff(t, e.s, e.tenant, e.page, "u1@elimaz.test", "conversation.read"),
		convtest.Staff(t, e.s, e.tenant, e.page, "u2@elimaz.test", "conversation.read"),
		convtest.Staff(t, e.s, e.tenant, e.page, "u3@elimaz.test", "conversation.read"),
	}
	hook := assign.RoundRobin{}
	n := 0
	run := func() (uuid.UUID, bool) {
		n++
		c := convtest.Conversation(t, e.s, e.tenant, e.page, fmt.Sprintf("rr-%d", n))
		if err := e.tx(func(tx pgx.Tx) error { return hook.OnNewConversation(t.Context(), tx, c) }); err != nil {
			t.Fatal(err)
		}
		var to *uuid.UUID
		e.s.Exec(t, e.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `SELECT assignee_id FROM conversation.conversations WHERE id = $1`, c).Scan(&to)
		})
		if to == nil {
			return uuid.Nil, false
		}
		return *to, true
	}
	if _, got := run(); got {
		t.Fatal("chưa có dòng cài đặt mà vẫn giao")
	}
	settings := func(enabled bool) {
		e.s.Exec(t, e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), `INSERT INTO conversation.page_settings (tenant_id, channel_account_id,
				round_robin_enabled, round_robin_user_ids) VALUES ($1, $2, $3, $4)
				ON CONFLICT (tenant_id, channel_account_id) DO UPDATE SET round_robin_enabled = EXCLUDED.round_robin_enabled`,
				e.tenant, e.page, enabled, u)
			return err //nolint:wrapcheck // test
		})
	}
	settings(true)
	for i, want := range []uuid.UUID{u[0], u[1], u[2], u[0], u[1], u[2]} {
		if got, _ := run(); got != want {
			t.Fatalf("hội thoại %d: %v, muốn %v", i+1, got, want)
		}
	}
	e.s.SetStatus(t, e.tenant, u[1], "locked")
	for i, want := range []uuid.UUID{u[0], u[2]} {
		if got, _ := run(); got != want {
			t.Fatalf("sau khi khóa u2, hội thoại %d: %v, muốn %v", i+1, got, want)
		}
	}
	if n := e.s.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.conversation_assignments
		WHERE reason = 'round_robin' AND actor_id IS NULL`); n != 8 {
		t.Fatalf("dòng lịch sử xoay vòng = %d", n)
	}
	// Hội thoại đã có người phụ trách (gọi lại hook, ví dụ tin thứ hai) → không đổi.
	c := convtest.Conversation(t, e.s, e.tenant, e.page, "rr-assigned")
	a := e.ids[an]
	if err := e.assign(manager, c, &a, 1); err != nil {
		t.Fatal(err)
	}
	if err := e.tx(func(tx pgx.Tx) error { return hook.OnNewConversation(t.Context(), tx, c) }); err != nil {
		t.Fatal(err)
	}
	if n := e.s.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.conversations WHERE id = $1 AND assignee_id = $2`,
		c, a); n != 1 {
		t.Fatal("hook đổi người phụ trách của hội thoại đã có")
	}
	settings(false)
	if _, got := run(); got {
		t.Fatal("tắt xoay vòng mà vẫn giao")
	}
}
