package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// [Task 501] listPagePosts: bài của Page (mới gặp trước, theo id), đủ trường (mã ngoài, loại, đoạn, giờ đăng, đường
// dẫn; trường trống không có), trang theo nextCursor; bài Page khác không lẫn; người chỉ đọc Page 1 xem Page 2 → 404;
// con trỏ sai → 400; Page không có → 404.
func TestPagePostsAPI(t *testing.T) {
	t.Parallel()
	e := newAssignAPIEnv(t)
	posted := time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)
	var ids []uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		for i, ext := range []string{"p-1", "p-2", "p-3"} {
			var id uuid.UUID
			if err := tx.QueryRow(t.Context(), `INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id,
				kind, permalink, snippet, created_time) VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'post',
				CASE WHEN $3 THEN 'https://www.facebook.com/' || $2 END, CASE WHEN $3 THEN 'Áo len' END,
				CASE WHEN $3 THEN $4::timestamptz END) RETURNING id`, e.page1, ext, i == 2, posted).Scan(&id); err != nil {
				return err //nolint:wrapcheck // test
			}
			ids = append(ids, id)
		}
		_, err := tx.Exec(t.Context(), `INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id)
			VALUES (current_setting('app.tenant_id')::uuid, $1, 'p-khac')`, e.page2)
		return err //nolint:wrapcheck // test
	})
	base := "/api/v1/channels/" + e.page1.String() + "/posts"
	code, s := e.do(http.MethodGet, base+"?limit=2", cvAn, "", "")
	items, _ := s["items"].([]any)
	if code != http.StatusOK || len(items) != 2 || s["nextCursor"] == nil {
		t.Fatalf("trang 1: %d %v", code, s)
	}
	first, _ := items[0].(map[string]any)
	second, _ := items[1].(map[string]any)
	if first["id"] != ids[2].String() || first["externalId"] != "p-3" || first["kind"] != "post" ||
		first["snippet"] != "Áo len" || first["permalink"] != "https://www.facebook.com/p-3" ||
		first["postedAt"] != "2026-11-19T08:00:00Z" || second["externalId"] != "p-2" || second["snippet"] != nil ||
		second["postedAt"] != nil || second["permalink"] != nil {
		t.Fatalf("dòng: %v | %v", first, second)
	}
	cur, _ := s["nextCursor"].(string)
	code, s = e.do(http.MethodGet, base+"?limit=2&cursor="+cur, cvAn, "", "")
	if items, _ := s["items"].([]any); code != http.StatusOK || len(items) != 1 || s["nextCursor"] != nil {
		t.Fatalf("trang 2: %d %v", code, s)
	} else if it, _ := items[0].(map[string]any); it["externalId"] != "p-1" {
		t.Fatalf("trang 2: %v", it)
	}
	expect(t, "Page ngoài phạm vi", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/channels/"+e.page2.String()+"/posts",
		cvAn, "", ""), http.StatusNotFound, "not_found")
	expect(t, "con trỏ sai", serveAs(e.srv, e.keys, http.MethodGet, base+"?cursor=abc", cvAn, "", ""),
		http.StatusBadRequest, "invalid_input")
	expect(t, "Page không có", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/channels/"+uuid.NewString()+"/posts",
		cvManager, "", ""), http.StatusNotFound, "not_found")
}

// [Task 501] Chi tiết hội thoại comment trả thẻ bài (postedAt, permalink, imageFileId) và newLead (CH08); hội thoại
// không tạo khách → không có newLead.
func TestConversationSummary_PostCardAndNewLead(t *testing.T) {
	t.Parallel()
	e := newAssignAPIEnv(t)
	img := uuid.New()
	var cust uuid.UUID
	if err := tenancy.WithTenant(t.Context(), e.staff.Owner, e.tenant, func(tx pgx.Tx) (err error) {
		cust, err = customer.Create(t.Context(), tx, "Chị Mới")
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	conv := func(created bool) (id uuid.UUID) {
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `WITH p AS (
					INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id, kind, permalink, created_time,
						image_file_id)
					VALUES (current_setting('app.tenant_id')::uuid, $1, gen_random_uuid()::text, 'post',
						'https://www.facebook.com/p', '2026-11-19T08:00:00Z', $3) RETURNING id)
				INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind, external_thread_ref,
					post_id, last_message_at, created_customer)
				SELECT current_setting('app.tenant_id')::uuid, $1, $2, 'comment', gen_random_uuid()::text, p.id, now(), $4
				FROM p RETURNING id`, e.page1, cust, img, created).Scan(&id)
		})
		return id
	}
	for _, created := range []bool{true, false} {
		code, s := e.do(http.MethodGet, "/api/v1/conversations/"+conv(created).String(), cvAn, "", "")
		c, _ := s["conversation"].(map[string]any)
		post, _ := c["post"].(map[string]any)
		wantLead := any(nil)
		if created {
			wantLead = true
		}
		if code != http.StatusOK || c["newLead"] != wantLead || post["imageFileId"] != img.String() ||
			post["permalink"] != "https://www.facebook.com/p" || post["postedAt"] != "2026-11-19T08:00:00Z" {
			t.Fatalf("created_customer=%v: %d %v", created, code, s)
		}
	}
}
