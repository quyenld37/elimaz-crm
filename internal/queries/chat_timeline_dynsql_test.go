package queries_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries"
	"elimaz.com.vn/crm/internal/testkit"
)

// ChatTimeline (Task 169, C7): hồ sơ A (gốc) và B (gộp vào A); hội thoại inbox của A trên Page P1 (01/11), comment của B
// trên Page P2 (02/11), cuộc gọi gắn hội thoại P1 (03/11, có ghi chú), cuộc gọi gắn hội thoại P2 (04/11), cuộc gọi
// không gắn hội thoại (05/11). Mọi Page → 5 mục giảm dần; chỉ P1 → 3 mục; không Page nào → chỉ cuộc gọi lẻ; trang
// 2 + 3 theo khóa không trùng, không sót; tóm tắt không có ghi chú.
func TestChatTimeline(t *testing.T) {
	t.Parallel()
	f := seedChatTimeline(t)
	s, tenant, user, p1, a, b, inbox, comment, calls := f.staff, f.tenant, f.user, f.p1, f.a, f.b, f.inbox, f.comment, f.calls
	ctx := t.Context()
	read := func(pages []uuid.UUID, before *queries.TimelineKey, limit int) []queries.ChatTimelineRow {
		t.Helper()
		var out []queries.ChatTimelineRow
		if err := tenancy.WithTenant(ctx, s.App, tenant, func(tx pgx.Tx) error {
			var err error
			out, err = queries.ChatTimeline(ctx, tx, []uuid.UUID{a, b}, pages, before, limit)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	refs := func(rows []queries.ChatTimelineRow) []uuid.UUID {
		out := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.RefID)
		}
		return out
	}
	all := read(nil, nil, 50)
	if got, want := refs(all), []uuid.UUID{calls[2], calls[1], calls[0], comment, inbox}; !slices.Equal(got, want) {
		t.Fatalf("mọi Page: %v, mong %v", got, want)
	}
	if r := all[4]; r.Kind != queries.TimelineConversation || r.ActorID != nil || r.Summary["conversationId"] != inbox.String() ||
		r.Summary["pageId"] != p1.String() || r.Summary["kind"] != "inbox" || len(r.Summary) != 3 {
		t.Errorf("mục hội thoại: %+v", r)
	}
	if r := all[2]; r.Kind != queries.TimelineCall || r.ActorID == nil || *r.ActorID != user || r.Summary["outcome"] != "no_answer" ||
		r.Summary["conversationId"] != inbox.String() || len(r.Summary) != 2 {
		t.Errorf("mục cuộc gọi: %+v", r)
	}
	if r := all[0]; len(r.Summary) != 1 {
		t.Errorf("cuộc gọi lẻ có tóm tắt thừa: %+v", r)
	}
	body, _ := json.Marshal(all)
	if strings.Contains(string(body), "bi-mat") {
		t.Errorf("tóm tắt lộ nội dung: %s", body)
	}
	if got, want := refs(read([]uuid.UUID{p1}, nil, 50)), []uuid.UUID{calls[2], calls[0], inbox}; !slices.Equal(got, want) {
		t.Errorf("chỉ Page P1: %v, mong %v", got, want)
	}
	if got, want := refs(read([]uuid.UUID{}, nil, 50)), []uuid.UUID{calls[2]}; !slices.Equal(got, want) {
		t.Errorf("không Page nào: %v, mong %v", got, want)
	}
	page1 := read(nil, nil, 2)
	last := page1[len(page1)-1]
	page2 := read(nil, &queries.TimelineKey{At: last.At, Kind: last.Kind, RefID: last.RefID}, 50)
	if got := append(refs(page1), refs(page2)...); !slices.Equal(got, refs(all)) {
		t.Errorf("hai trang: %v, mong %v", got, refs(all))
	}
}

// chatTimelineFixture là dữ liệu của TestChatTimeline (mô tả ở đó).
type chatTimelineFixture struct {
	staff                                  *testkit.Staff
	tenant, user, p1, a, b, inbox, comment uuid.UUID
	calls                                  []uuid.UUID
}

func seedChatTimeline(t *testing.T) chatTimelineFixture {
	t.Helper()
	s := testkit.OpenStaff(t)
	tenant := s.Tenant(t, "A")
	user := s.User(t, tenant, "sale@elimaz.test", "active")
	p1 := s.ChannelAccount(t, tenant, "facebook", "page-1", "Page 1")
	p2 := s.ChannelAccount(t, tenant, "facebook", "page-2", "Page 2")
	f := chatTimelineFixture{staff: s, tenant: tenant, user: user, p1: p1, calls: make([]uuid.UUID, 3)}
	a, b, inbox, comment, calls := &f.a, &f.b, &f.inbox, &f.comment, f.calls
	ctx := t.Context()
	s.Exec(t, tenant, func(tx pgx.Tx) error {
		for _, id := range []*uuid.UUID{a, b} {
			if err := tx.QueryRow(ctx, `INSERT INTO customer.customers (tenant_id, full_name, name_search)
				VALUES (current_setting('app.tenant_id')::uuid, 'Lan', 'lan') RETURNING id`).Scan(id); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE customer.customers SET root_id = $1 WHERE id = $2`, *a, *b); err != nil {
			return err //nolint:wrapcheck // test
		}
		var post uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id)
			VALUES (current_setting('app.tenant_id')::uuid, $1, 'post') RETURNING id`, p2).Scan(&post); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, `INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind,
			external_thread_ref, last_message_at, created_at, last_snippet) VALUES (current_setting('app.tenant_id')::uuid, $1, $2,
			'inbox', 'psid', now(), '2026-11-01Z', 'tin-bi-mat') RETURNING id`, p1, *a).Scan(inbox); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, `INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind,
			external_thread_ref, post_id, last_message_at, created_at) VALUES (current_setting('app.tenant_id')::uuid, $1, $2,
			'comment', 'post:psid', $3, now(), '2026-11-02Z') RETURNING id`, p2, *b, post).Scan(comment); err != nil {
			return err //nolint:wrapcheck // test
		}
		for i, c := range []struct {
			conv any
			day  string
		}{{*inbox, "2026-11-03Z"}, {*comment, "2026-11-04Z"}, {nil, "2026-11-05Z"}} {
			if err := tx.QueryRow(ctx, `INSERT INTO conversation.call_logs (tenant_id, customer_id, conversation_id, outcome,
				note, created_by, created_at) VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'no_answer', 'ghi-chu-bi-mat',
				$3, $4) RETURNING id`, *b, c.conv, user, c.day).Scan(&calls[i]); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	return f
}
