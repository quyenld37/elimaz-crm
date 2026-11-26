package conversation_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// storeSQL trả nguyên văn câu sqlc name (hằng trong internal/store/*.sql.go, gồm dòng "-- name:") để chạy EXPLAIN
// đúng câu ứng dụng chạy.
func storeSQL(t *testing.T, name string) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("internal", "store", "*.sql.go"))
	if err != nil {
		t.Fatal(err)
	}
	marker := "const " + name + " = `"
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, rest, found := strings.Cut(string(b), marker); found {
			body, _, _ := strings.Cut(rest, "`")
			return body
		}
	}
	t.Fatalf("không thấy câu %s trong store", name)
	return ""
}

// explainSeed (04 mục 7): tenant A có 2 Page, 1.000 khách, 20 nhân viên, 100 bài viết, 200.000 hội thoại (10% comment,
// rải bốn trạng thái, 19/20 có người phụ trách) và 500.000 tin (5% là comment live theo bài); tenant B 2.000 hội thoại,
// 5.000 tin; ANALYZE. Chạy bằng chủ bảng trong ngữ cảnh từng tenant.
func explainSeed(a, b, pageA1, pageA2, pageB uuid.UUID) testkit.Seed {
	q := []string{
		"SET ROLE crm_owner",
		fmt.Sprintf("INSERT INTO platform.tenants (id, name) VALUES ('%s', 'A'), ('%s', 'B')", a, b),
	}
	for _, c := range []struct {
		tenant         uuid.UUID
		pages          string
		convs, msgs    int
		users, posts   int
		customers, tag string
	}{
		{a, fmt.Sprintf("ARRAY['%s','%s']::uuid[]", pageA1, pageA2), 200000, 500000, 20, 100, "1000", "a"},
		{b, fmt.Sprintf("ARRAY['%s']::uuid[]", pageB), 2000, 5000, 2, 10, "50", "b"},
	} {
		q = append(q,
			fmt.Sprintf("SELECT set_config('app.tenant_id', '%s', false)", c.tenant),
			`INSERT INTO customer.customers (tenant_id, full_name, name_search)
				SELECT current_setting('app.tenant_id')::uuid, 'khách ' || g, 'khach ' || g
				FROM generate_series(1, `+c.customers+`) AS g`,
			fmt.Sprintf(`INSERT INTO identity.users (tenant_id, email, display_name)
				SELECT current_setting('app.tenant_id')::uuid, 'nv' || g || '@%s.test', 'nv ' || g
				FROM generate_series(1, %d) AS g`, c.tag, c.users),
			`INSERT INTO catalog.sellers (tenant_id, code, name, kind)
				VALUES (current_setting('app.tenant_id')::uuid, 'ELIMAZ', 'ELIMAZ', 'own')`,
			// Page là tài khoản kênh thật (khóa ngoại 0412, Task 153).
			fmt.Sprintf(`INSERT INTO channel.channel_accounts (id, tenant_id, seller_id, channel, external_id, name)
				SELECT p, s.tenant_id, s.id, 'facebook', p::text, 'Page'
				FROM unnest(%s) AS p, catalog.sellers AS s
				WHERE s.tenant_id = current_setting('app.tenant_id')::uuid AND s.code = 'ELIMAZ'`, c.pages),
			fmt.Sprintf(`INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id, kind)
				SELECT current_setting('app.tenant_id')::uuid, (%s)[1 + g %% cardinality(%s)], 'post-' || g, 'live'
				FROM generate_series(1, %d) AS g`, c.pages, c.pages, c.posts),
			fmt.Sprintf(`INSERT INTO conversation.conversations
				(tenant_id, channel_account_id, customer_id, kind, external_thread_ref, post_id, status, assignee_id,
				 last_message_at, unread_count)
				SELECT current_setting('app.tenant_id')::uuid, (%[1]s)[1 + g %% cardinality(%[1]s)], cu.ids[1 + g %% cardinality(cu.ids)],
				       CASE WHEN g %% 10 = 0 THEN 'comment' ELSE 'inbox' END, 'thread-' || g,
				       CASE WHEN g %% 10 = 0 THEN po.ids[1 + g %% cardinality(po.ids)] END,
				       (ARRAY['open','pending','done','spam'])[1 + (g / 7) %% 4],
				       CASE WHEN g %% 20 <> 0 THEN us.ids[1 + g %% cardinality(us.ids)] END,
				       timestamptz '2026-01-01' + g * interval '1 minute', g %% 3
				FROM generate_series(1, %[2]d) AS g,
				     (SELECT array_agg(id ORDER BY id) AS ids FROM customer.customers) AS cu,
				     (SELECT array_agg(id ORDER BY id) AS ids FROM identity.users) AS us,
				     (SELECT array_agg(id ORDER BY id) AS ids FROM conversation.posts) AS po`, c.pages, c.convs),
			fmt.Sprintf(`INSERT INTO conversation.messages
				(tenant_id, conversation_id, channel_account_id, external_id, direction, kind, origin, post_id, body,
				 is_live, sent_at)
				SELECT cv.tenant_id, cv.id, cv.channel_account_id, 'm-' || g, 'in',
				       CASE WHEN cv.kind = 'comment' THEN 'comment' ELSE 'message' END, 'customer', cv.post_id,
				       'tin ' || g, cv.kind = 'comment' AND g %% 2 = 1,
				       timestamptz '2026-01-01' + g * interval '10 second'
				FROM generate_series(1, %d) AS g
				JOIN (SELECT *, row_number() OVER (ORDER BY id) AS rn FROM conversation.conversations) AS cv
				  ON cv.rn = 1 + (g::bigint * 7919) %% %d`,
				c.msgs, c.convs),
		)
	}
	return testkit.Seed{Name: "conversation-core-plan", SQL: append(q,
		"ANALYZE conversation.posts", "ANALYZE conversation.conversations", "ANALYZE conversation.messages")}
}

// [Task 151, 04 mục 7] Bốn truy vấn lõi bằng crm_app trên tenant A (200.000 hội thoại trên 2 Page, 500.000 tin):
// danh sách theo Page và trạng thái; theo người phụ trách; tin của một hội thoại; comment live theo bài → không Seq
// Scan, và mỗi câu trả đúng số dòng mong đợi (không rỗng do RLS).
func TestCoreQueries_Plan(t *testing.T) {
	t.Parallel()
	a, b := testkit.FixedUUID("conversation-core-plan/A"), testkit.FixedUUID("conversation-core-plan/B")
	pageA1, pageA2 := testkit.FixedUUID("conversation-core-plan/pageA1"), testkit.FixedUUID("conversation-core-plan/pageA2")
	pageB := testkit.FixedUUID("conversation-core-plan/pageB")
	staff := testkit.OpenStaffSeeded(t, explainSeed(a, b, pageA1, pageA2, pageB))
	ctx := t.Context()
	var conv, assignee, post uuid.UUID
	staff.Exec(t, a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT
			(SELECT id FROM conversation.conversations WHERE kind = 'inbox' AND external_thread_ref = 'thread-1001'),
			(SELECT assignee_id FROM conversation.conversations WHERE external_thread_ref = 'thread-1001'),
			(SELECT post_id FROM conversation.messages WHERE is_live LIMIT 1)`).Scan(&conv, &assignee, &post)
	})
	if conv == uuid.Nil || assignee == uuid.Nil || post == uuid.Nil {
		t.Fatalf("dữ liệu mẫu thiếu: hội thoại %s, người phụ trách %s, bài live %s", conv, assignee, post)
	}
	far := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	maxID := uuid.Max
	for _, c := range []struct {
		name string
		args []any
	}{
		{"listConversationsByPage", []any{pageA1, "open", far, maxID, int32(50)}},
		{"listConversationsByAssignee", []any{assignee, "open", far, maxID, int32(50)}},
		{"listMessages", []any{conv, far, maxID, int32(50)}},
		{"listLiveComments", []any{post, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), uuid.Nil, int32(200)}},
	} {
		sql := storeSQL(t, c.name)
		plan, err := testkit.ExplainAsApp(ctx, staff.App, a, sql, c.args...)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		testkit.RequireNoSeqScan(t, plan)
		var n int
		if err := tenancy.WithTenant(ctx, staff.App, a, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM ("+sql+") AS q", c.args...).Scan(&n)
		}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if n == 0 {
			t.Errorf("%s: 0 dòng, mong có dữ liệu", c.name)
		}
	}
}
