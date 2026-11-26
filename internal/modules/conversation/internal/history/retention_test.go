package history_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/history"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// retentionNowUnix là 02:40 giờ Việt Nam (19:40 UTC hôm trước): trong khung chạy của lịch dọn.
const retentionNowUnix = 1795635600 // 2026-11-25T19:40:00Z

// months lùi n tháng từ now() (cùng một đồng hồ với việc dọn).
func months(n int) time.Time { return now().AddDate(0, -n, 0) }

func now() time.Time { return time.Unix(retentionNowUnix, 0).UTC() }

// Giữ 24 tháng (Q17): tin 25 tháng tuổi bị xóa cùng lịch sử sửa, tệp đính kèm chỉ nó dùng được đánh dấu xóa (tệp còn
// tin mới dùng thì không); tin 23 tháng còn; hội thoại chỉ có tin quá hạn bị xóa cùng nhãn, lịch sử phân công; hội
// thoại có hẹn gọi lại thì giữ; chạy lần hai xóa 0.
func TestRetention(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.d.Now = now
	only, shared := e.file(), e.file()
	keep := e.thread("keep", months(1))
	oldMsg := e.message(keep, "old-1", months(25), only, shared)
	e.revision(oldMsg)
	e.message(keep, "mid-1", months(23))
	e.message(keep, "new-1", months(1), shared)
	gone := e.thread("gone", months(25))
	e.message(gone, "gone-1", months(26))
	e.tagAndAssign(gone)
	reminded := e.thread("reminded", months(25))
	e.message(reminded, "rem-1", months(25))
	staffUser := e.staff.User(t, e.tenant, "sale@elimaz.test", "active")
	e.exec(`INSERT INTO conversation.reminders (tenant_id, conversation_id, user_id, remind_at, note, status, source,
		created_by) VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, 'gọi lại', 'done', 'manual', $2)`,
		reminded, staffUser, months(25))

	res := e.runRetention(e.retention(5000))
	if res.Messages != 3 || res.More {
		t.Fatalf("lần 1: %+v, mong xóa 3 tin, không còn lô sau", res)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE external_id IN ('old-1', 'gone-1', 'rem-1')`); n != 0 {
		t.Errorf("còn %d tin quá hạn", n)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE external_id IN ('mid-1', 'new-1')`); n != 2 {
		t.Errorf("tin 23 tháng, 1 tháng: còn %d, mong 2", n)
	}
	if n := e.count(`SELECT count(*) FROM conversation.message_revisions WHERE message_id = $1`, oldMsg); n != 0 {
		t.Errorf("còn %d dòng lịch sử sửa của tin đã xóa", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.files WHERE id = $1 AND deleted_at IS NOT NULL`, only); n != 1 {
		t.Errorf("tệp chỉ tin cũ dùng chưa bị đánh dấu xóa")
	}
	if n := e.count(`SELECT count(*) FROM platform.files WHERE id = $1 AND deleted_at IS NULL`, shared); n != 1 {
		t.Errorf("tệp tin mới còn dùng bị đánh dấu xóa")
	}
	checkConversations(t, e, keep, gone, reminded)

	if res := e.runRetention(e.retention(5000)); res.Messages != 0 || res.Conversations != 0 {
		t.Errorf("lần 2: %+v, mong xóa 0", res)
	}
}

func checkConversations(t *testing.T, e *env, keep, gone, reminded uuid.UUID) {
	t.Helper()
	if n := e.count(`SELECT count(*) FROM conversation.conversations WHERE id = ANY($1)`, []uuid.UUID{keep, reminded}); n != 2 {
		t.Errorf("hội thoại còn tin mới, có hẹn gọi lại: còn %d, mong 2", n)
	}
	for _, q := range []string{
		`SELECT count(*) FROM conversation.conversations WHERE id = $1`,
		`SELECT count(*) FROM conversation.conversation_tag_links WHERE conversation_id = $1`,
		`SELECT count(*) FROM conversation.conversation_assignments WHERE conversation_id = $1`,
	} {
		if n := e.count(q, gone); n != 0 {
			t.Errorf("%s: %d dòng của hội thoại quá hạn, mong 0", q, n)
		}
	}
}

// Lô đầy → ghi một việc dọn tiếp theo (không chờ ngày sau); việc tiếp theo chạy cả ngoài khung giờ.
func TestRetention_Continues(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.d.Now = now
	c := e.thread("batch", months(1))
	e.message(c, "b-1", months(30))
	e.message(c, "b-2", months(29))
	res := e.runRetention(e.retention(1))
	if res.Messages != 1 || !res.More {
		t.Fatalf("lô 1: %+v, mong 1 tin, còn lô sau", res)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1`, history.RetentionKind); n != 1 {
		t.Fatalf("%d việc dọn tiếp theo, mong 1", n)
	}
	e.d.Now = func() time.Time { return now().Add(8 * time.Hour) } // 10:40 giờ Việt Nam
	var payload json.RawMessage
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT payload FROM platform.jobs WHERE kind = $1`, history.RetentionKind).Scan(&payload)
	})
	res = e.runRetentionPayload(e.retention(1), payload)
	if res.Messages != 1 || res.Skipped != "" {
		t.Errorf("lô 2 (việc tiếp theo, ngoài khung giờ): %+v, mong xóa 1", res)
	}
}

// Lịch chạy mỗi giờ nhưng chỉ dọn trong khung 2:00–2:59 giờ Việt Nam.
func TestRetention_OutsideHours(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.d.Now = func() time.Time { return now().Add(time.Hour) } // 03:40 giờ Việt Nam
	c := e.thread("late", months(1))
	e.message(c, "late-1", months(30))
	if res := e.runRetention(e.retention(5000)); res.Skipped != "outside_hours" || res.Messages != 0 {
		t.Errorf("ngoài khung giờ: %+v", res)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE external_id = 'late-1'`); n != 1 {
		t.Errorf("tin bị xóa ngoài khung giờ")
	}
}

// DELETE trên message_revisions (crm_app) khi chưa bật app.retention → bị trigger chặn (42501).
func TestRetention_RevisionsNeedFlag(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.thread("flag", months(1))
	e.revision(e.message(c, "flag-1", months(1)))
	err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `DELETE FROM conversation.message_revisions`)
		return err //nolint:wrapcheck // test
	})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Errorf("DELETE không bật cờ: %v, mong 42501", err)
	}
}

// retentionResult là kết quả việc dọn (jobs.result).
type retentionResult struct {
	Messages      int    `json:"messages"`
	Conversations int    `json:"conversations"`
	Files         int    `json:"files"`
	More          bool   `json:"more"`
	Skipped       string `json:"skipped"`
}

func (e *env) retention(batch int) queue.Handler { //nolint:ireturn // handler việc nền là interface của queue
	return history.NewRetentionHandler(e.d, batch)
}

// runRetention chạy việc dọn của lịch (payload kỳ) trong một transaction tenant bằng crm_app.
func (e *env) runRetention(h queue.Handler) retentionResult {
	e.t.Helper()
	return e.runRetentionPayload(h, json.RawMessage(`{"slot":"2026-11-25T19:00:00Z"}`))
}

func (e *env) runRetentionPayload(h queue.Handler, payload json.RawMessage) retentionResult {
	e.t.Helper()
	tenant := e.tenant
	var raw json.RawMessage
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, tenant, func(tx pgx.Tx) error {
		var err error
		raw, err = h.Handle(e.t.Context(), queue.Job{
			ID: uuid.New(), TenantID: &tenant, Kind: history.RetentionKind,
			Payload: payload, Attempts: 1,
		}, tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("việc dọn: %v", err)
	}
	var res retentionResult
	if err := json.Unmarshal(raw, &res); err != nil {
		e.t.Fatalf("kết quả việc dọn %s: %v", raw, err)
	}
	return res
}

// exec chạy câu ghi sql bằng chủ bảng trong tenant A; câu không đổi dòng nào là lỗi của dữ liệu thử.
func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		tag, err := tx.Exec(e.t.Context(), sql, args...)
		if err == nil && tag.RowsAffected() == 0 {
			e.t.Errorf("câu thử không ghi dòng nào: %s", sql)
		}
		return err //nolint:wrapcheck // test
	})
}

// thread chèn khách và hội thoại hộp thư (mã luồng ref) trên Page, tin cuối lúc last.
func (e *env) thread(ref string, last time.Time) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		var cust uuid.UUID
		if err := tx.QueryRow(e.t.Context(), `INSERT INTO customer.customers (tenant_id, full_name, name_search)
			VALUES (current_setting('app.tenant_id')::uuid, 'Chị Hoa', 'chi hoa') RETURNING id`).Scan(&cust); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(e.t.Context(), `INSERT INTO conversation.conversations (tenant_id, channel_account_id,
			customer_id, kind, external_thread_ref, last_message_at, last_customer_message_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'inbox', $3, $4, $4) RETURNING id`,
			e.page, cust, ref, last.UTC()).Scan(&id)
	})
	return id
}

// message chèn tin khách mã ngoài ext lúc at vào hội thoại conv, đính kèm các tệp files; trả mã tin.
func (e *env) message(conv uuid.UUID, ext string, at time.Time, files ...uuid.UUID) uuid.UUID {
	e.t.Helper()
	atts := make([]map[string]any, 0, len(files))
	for i, f := range files {
		atts = append(atts, map[string]any{"index": i, "type": "image", "status": "ready", "fileId": f.String()})
	}
	raw, err := json.Marshal(atts)
	if err != nil {
		e.t.Fatal(err)
	}
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `INSERT INTO conversation.messages (tenant_id, conversation_id,
			channel_account_id, external_id, direction, kind, origin, body, attachments, sent_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, 'in', 'message', 'customer', 'tin', $4, $5)
			RETURNING id`, conv, e.page, ext, raw, at.UTC()).Scan(&id)
	})
	return id
}

// revision chèn một dòng lịch sử sửa của tin msg.
func (e *env) revision(msg uuid.UUID) {
	e.t.Helper()
	e.exec(`INSERT INTO conversation.message_revisions (tenant_id, message_id, body, source)
		VALUES (current_setting('app.tenant_id')::uuid, $1, 'bản cũ', 'customer')`, msg)
}

// tagAndAssign gắn một nhãn và một dòng phân công cho hội thoại conv.
func (e *env) tagAndAssign(conv uuid.UUID) {
	e.t.Helper()
	e.exec(`WITH t AS (INSERT INTO conversation.tags (tenant_id, name, color)
			VALUES (current_setting('app.tenant_id')::uuid, 'Khách cũ', 'gray') RETURNING id)
		INSERT INTO conversation.conversation_tag_links (tenant_id, conversation_id, tag_id)
		SELECT current_setting('app.tenant_id')::uuid, $1, id FROM t`, conv)
	e.exec(`INSERT INTO conversation.conversation_assignments (tenant_id, conversation_id, reason)
		VALUES (current_setting('app.tenant_id')::uuid, $1, 'unassign')`, conv)
}

// file chèn một tệp đính kèm chat (ready) của tenant A; trả mã.
func (e *env) file() uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	e.exec(`INSERT INTO platform.files (id, tenant_id, purpose, bucket, key, original_name, declared_type, size_bytes,
		content_sha256, status)
		VALUES ($1::uuid, current_setting('app.tenant_id')::uuid, 'chat_outgoing', 'originals', 'tmp/' || $1::text,
		'a.jpg', 'image/jpeg', 10, decode(repeat('cd', 32), 'hex'), 'ready')`, id)
	return id
}
