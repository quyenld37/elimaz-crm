package query_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// msg chèn một tin của hội thoại conv lúc at; kind message/comment/private_reply; staff khác nil: tin nhân viên.
type msg struct {
	conv, page    uuid.UUID
	ext, kind     string
	parentExt     string
	at            time.Time
	staff         *uuid.UUID
	sendStatus    string
	attachments   string
	hidden        bool
	removed       bool
	deliveredRead bool
}

func (e *env) msg(m msg) uuid.UUID {
	e.t.Helper()
	dir, origin := "in", "customer"
	var status *string
	if m.staff != nil {
		dir, origin = "out", "staff"
		status = &m.sendStatus
	}
	if m.kind == "" {
		m.kind = "message"
	}
	if m.attachments == "" {
		m.attachments = "[]"
	}
	var ext, parent *string
	if m.ext != "" {
		ext = &m.ext
	}
	if m.parentExt != "" {
		parent = &m.parentExt
	}
	var id uuid.UUID
	e.scan(&id, `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, external_id,
		parent_external_id, direction, kind, origin, body, attachments, sender_staff_id, send_status, is_hidden, removed_at,
		delivered_at, read_at, sent_at)
		VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, $4, $5, $6, $7, 'nội dung', $8::jsonb, $9, $10, $11,
		        CASE WHEN $12 THEN $13::timestamptz END, CASE WHEN $14 THEN $13::timestamptz END,
		        CASE WHEN $14 THEN $13::timestamptz END, $13)
		RETURNING id`, m.conv, m.page, ext, parent, dir, m.kind, origin, m.attachments, m.staff, status, m.hidden,
		m.removed, m.at, m.deliveredRead)
	return id
}

func (e *env) messages(u identity.AuthUser, id uuid.UUID, before, after *conversation.PageCursor, limit int,
) (conversation.MessagePage, error) {
	e.t.Helper()
	var p conversation.MessagePage
	err := e.read(func(tx pgx.Tx) (err error) {
		p, err = conversation.ListMessages(e.t.Context(), tx, u, id, before, after, limit)
		return err //nolint:wrapcheck // test
	})
	return p, err
}

func msgIDs(p conversation.MessagePage) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(p.Items))
	for _, m := range p.Items {
		out = append(out, m.ID)
	}
	return out
}

// [Task 159] Tin theo trang: 7 tin, limit 3 → trang mới nhất [5 6 7] theo thứ tự thời gian, có Older; before Older →
// [2 3 4]; rồi [1], hết Older. after mốc tin 4 → [5 6 7]; after mốc tin mới nhất → rỗng, Newer giữ nguyên. Gửi cả
// before và after → ErrInvalidInput.
func TestMessages_Paging(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	u := e.user("tin@elimaz.test", e.pageA, "conversation.read")
	id := e.conv(conv{page: e.pageA, customer: e.customer("Khách tin", "")})
	base := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	m := make([]uuid.UUID, 0, 7)
	for i := range 7 {
		m = append(m, e.msg(msg{conv: id, page: e.pageA, at: base.Add(time.Duration(i) * time.Second)}))
	}
	p, err := e.messages(u, id, nil, nil, 3)
	if err != nil || !slices.Equal(msgIDs(p), m[4:7]) || p.Older == nil || p.Newer == nil || p.Newer.ID != m[6] {
		t.Fatalf("trang mới nhất: %v (lỗi %v)", msgIDs(p), err)
	}
	p, err = e.messages(u, id, p.Older, nil, 3)
	if err != nil || !slices.Equal(msgIDs(p), m[1:4]) || p.Older == nil {
		t.Fatalf("trang trước: %v (lỗi %v)", msgIDs(p), err)
	}
	p, err = e.messages(u, id, p.Older, nil, 3)
	if err != nil || !slices.Equal(msgIDs(p), m[0:1]) || p.Older != nil {
		t.Fatalf("trang đầu: %v, Older %v (lỗi %v)", msgIDs(p), p.Older, err)
	}
	after := &conversation.PageCursor{CreatedAt: base.Add(3 * time.Second), ID: m[3]}
	p, err = e.messages(u, id, nil, after, 50)
	if err != nil || !slices.Equal(msgIDs(p), m[4:7]) || p.Newer.ID != m[6] {
		t.Fatalf("after tin 4: %v (lỗi %v)", msgIDs(p), err)
	}
	p, err = e.messages(u, id, nil, p.Newer, 50)
	if err != nil || len(p.Items) != 0 || p.Newer == nil || p.Newer.ID != m[6] {
		t.Fatalf("after tin mới nhất: %v, Newer %v (lỗi %v)", msgIDs(p), p.Newer, err)
	}
	if _, err := e.messages(u, id, after, after, 3); !errors.Is(err, conversation.ErrInvalidInput) {
		t.Fatalf("before + after: lỗi %v", err)
	}
}

// [Task 159; ghi chú Task 154] Cờ của tin: tin nhắn thường của khách → không có privateReply, isHidden/removed false,
// editedCount 0, đính kèm rỗng; tin nhân viên → người gửi, trạng thái gửi; comment có tin riêng đang gửi → privateReply
// {sent, mã}; comment không có (hay tin riêng đã lỗi) → none; comment trả lời → parentMessageId; comment đã ẩn, đã xóa,
// sửa 2 lần (một dòng lịch sử ẩn không tính) → cờ đúng; đính kèm có mã tệp và trạng thái.
func TestMessages_Flags(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	u := e.user("co@elimaz.test", uuid.Nil, "conversation.read")
	staff := e.user("nv-gui@elimaz.test", e.pageA, "conversation.read", "conversation.reply")
	cust := e.customer("Khách cờ", "")
	inbox := e.conv(conv{page: e.pageA, customer: cust})
	base := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	file := uuid.New()
	plain := e.msg(msg{conv: inbox, page: e.pageA, ext: "mid-1", at: base, deliveredRead: true})
	withFile := e.msg(msg{
		conv: inbox, page: e.pageA, ext: "mid-2", at: base.Add(time.Second),
		attachments: `[{"index":0,"type":"image","status":"ready","fileId":"` + file.String() + `"},{"index":1,"type":"sticker","status":"pending","fileId":null}]`,
	})
	out := e.msg(msg{conv: inbox, page: e.pageA, at: base.Add(2 * time.Second), staff: &staff.UserID, sendStatus: "sent"})

	cm := e.conv(conv{page: e.pageA, customer: cust, kind: "comment"})
	c1 := e.msg(msg{conv: cm, page: e.pageA, ext: "c-1", kind: "comment", at: base})
	c2 := e.msg(msg{
		conv: cm, page: e.pageA, ext: "c-2", kind: "comment", parentExt: "c-1", at: base.Add(time.Second),
		hidden: true, removed: true,
	})
	c3 := e.msg(msg{conv: cm, page: e.pageA, ext: "c-3", kind: "comment", at: base.Add(2 * time.Second)})
	pr := e.msg(msg{
		conv: cm, page: e.pageA, kind: "private_reply", parentExt: "c-1", at: base.Add(3 * time.Second),
		staff: &staff.UserID, sendStatus: "sent",
	})
	e.msg(msg{
		conv: cm, page: e.pageA, kind: "private_reply", parentExt: "c-3", at: base.Add(4 * time.Second),
		staff: &staff.UserID, sendStatus: "failed",
	})
	// Như đường comment vào (Task 154): mỗi dòng giữ nội dung trước lần đổi và cờ sau lần đổi; ẩn không đổi nội dung.
	for i, r := range []string{`'cũ 1', false, false`, `'cũ 2', false, false`, `'nội dung', true, false`} {
		e.exec(`INSERT INTO conversation.message_revisions (tenant_id, message_id, body, is_hidden, removed, source,
			recorded_at) VALUES (current_setting('app.tenant_id')::uuid, $1, `+r+`, 'customer', $2)`, c2,
			base.Add(time.Duration(10+i)*time.Second))
	}
	p, err := e.messages(u, inbox, nil, nil, 0)
	if err != nil || !slices.Equal(msgIDs(p), []uuid.UUID{plain, withFile, out}) {
		t.Fatalf("tin hộp thư: %v (lỗi %v)", msgIDs(p), err)
	}
	m0 := p.Items[0]
	if m0.PrivateReply != nil || m0.IsHidden || m0.Removed || m0.EditedCount != 0 || m0.Attachments == nil ||
		len(m0.Attachments) != 0 || m0.ParentID != nil || m0.SenderID != nil || m0.SendStatus != "" ||
		m0.Direction != "in" || m0.Kind != "message" || m0.Origin != "customer" || m0.Body != "nội dung" ||
		m0.DeliveredAt == nil || m0.ReadAt == nil {
		t.Fatalf("tin thường = %+v", m0)
	}
	if a := p.Items[1].Attachments; len(a) != 2 || a[0].FileID == nil || *a[0].FileID != file || a[0].Type != "image" ||
		a[0].Status != "ready" || a[1].FileID != nil || a[1].Status != "pending" {
		t.Fatalf("đính kèm = %+v", a)
	}
	if m2 := p.Items[2]; m2.SenderID == nil || *m2.SenderID != staff.UserID || m2.SenderStaffName == "" ||
		m2.SendStatus != "sent" || m2.Direction != "out" {
		t.Fatalf("tin nhân viên = %+v", m2)
	}

	p, err = e.messages(u, cm, nil, nil, 0)
	if err != nil || len(p.Items) != 5 {
		t.Fatalf("tin comment: %d (lỗi %v)", len(p.Items), err)
	}
	by := map[uuid.UUID]conversation.ChatMessage{}
	for _, m := range p.Items {
		by[m.ID] = m
	}
	if r := by[c1].PrivateReply; r == nil || r.State != "sent" || r.MessageID == nil || *r.MessageID != pr {
		t.Fatalf("comment 1 privateReply = %+v", r)
	}
	if r := by[c3].PrivateReply; r == nil || r.State != conversation.PrivateReplyNone || r.MessageID != nil {
		t.Fatalf("comment 3 (tin riêng lỗi) privateReply = %+v", r)
	}
	if m := by[c2]; m.ParentID == nil || *m.ParentID != c1 || !m.IsHidden || !m.Removed || m.EditedCount != 2 ||
		m.PrivateReply == nil || m.PrivateReply.State != conversation.PrivateReplyNone {
		t.Fatalf("comment 2 = %+v", m)
	}
	if by[pr].PrivateReply != nil {
		t.Fatalf("tin riêng không có privateReply: %+v", by[pr].PrivateReply)
	}
}

func (e *env) setStatus(u identity.AuthUser, id uuid.UUID, status string, version int32) error {
	e.t.Helper()
	return e.read(func(tx pgx.Tx) error {
		return conversation.Writer{}.SetStatus(e.t.Context(), tx, u, id, status, version)
	})
}

func (e *env) state(id uuid.UUID) (status string, version, unread int) {
	e.t.Helper()
	e.s.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT status, version, unread_count FROM conversation.conversations
			WHERE id = $1`, id).Scan(&status, &version, &unread)
	})
	return status, version, unread
}

// [Task 159] Đổi trạng thái: version cũ → ErrVersionConflict, không đổi; đúng version → done, version + 1, một audit
// chỉ có trạng thái; cùng trạng thái → không đổi version, không audit thêm; chỉ có conversation.read (không reply) →
// ErrNotFound; trạng thái lạ → ErrInvalidStatus.
func TestSetStatus(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	u := e.user("trang-thai@elimaz.test", e.pageA, "conversation.read", "conversation.reply")
	reader := e.user("chi-doc@elimaz.test", e.pageA, "conversation.read")
	id := e.conv(conv{page: e.pageA, customer: e.customer("Khách trạng thái", "")})
	if err := e.setStatus(u, id, conversation.StatusDone, 0); !errors.Is(err, conversation.ErrVersionConflict) {
		t.Fatalf("version cũ: lỗi %v", err)
	}
	if err := e.setStatus(reader, id, conversation.StatusDone, 1); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("không có reply: lỗi %v", err)
	}
	if err := e.setStatus(u, id, "archived", 1); !errors.Is(err, conversation.ErrInvalidStatus) {
		t.Fatalf("trạng thái lạ: lỗi %v", err)
	}
	if err := e.setStatus(u, id, conversation.StatusDone, 1); err != nil {
		t.Fatal(err)
	}
	if s, v, _ := e.state(id); s != "done" || v != 2 {
		t.Fatalf("sau khi đổi: %s v%d", s, v)
	}
	if err := e.setStatus(u, id, conversation.StatusDone, 2); err != nil {
		t.Fatal(err)
	}
	if s, v, _ := e.state(id); s != "done" || v != 2 {
		t.Fatalf("cùng trạng thái: %s v%d, muốn không đổi", s, v)
	}
	if n := e.s.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'conversation.status'
		AND entity_id = $1 AND after = '{"status": "done", "version": 2}'::jsonb`, id.String()); n != 1 {
		t.Fatalf("audit = %d, muốn 1", n)
	}
}

// [Task 159, hộp thư dùng chung] Đánh dấu đã đọc: 3 tin khách, unread 3 → đọc tới tin 2 → còn 1 (tin 3 đến sau lúc
// màn hình đọc); đọc tới tin 3 → 0; đọc lùi về tin 2 (mốc cũ của màn hình khác) → vẫn 0; version không đổi; tin của
// hội thoại khác → ErrNotFound.
func TestMarkRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	u := e.user("da-doc@elimaz.test", e.pageA, "conversation.read")
	cust := e.customer("Khách đọc", "")
	id := e.conv(conv{page: e.pageA, customer: cust, unread: 3})
	other := e.conv(conv{page: e.pageA, customer: cust})
	base := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	var m []uuid.UUID
	for i := range 3 {
		m = append(m, e.msg(msg{conv: id, page: e.pageA, at: base.Add(time.Duration(i) * time.Second)}))
	}
	foreign := e.msg(msg{conv: other, page: e.pageA, at: base})
	read := func(upTo uuid.UUID) error {
		return e.read(func(tx pgx.Tx) error { return conversation.Writer{}.MarkRead(t.Context(), tx, u, id, upTo) })
	}
	if err := read(m[1]); err != nil {
		t.Fatal(err)
	}
	if _, v, n := e.state(id); n != 1 || v != 1 {
		t.Fatalf("đọc tới tin 2: chưa đọc %d, version %d", n, v)
	}
	if err := read(m[2]); err != nil {
		t.Fatal(err)
	}
	if _, v, n := e.state(id); n != 0 || v != 1 {
		t.Fatalf("đọc tới tin 3: chưa đọc %d, version %d", n, v)
	}
	// Màn hình chậm của người khác gửi mốc cũ hơn (tin 2) sau khi đã đọc hết: số chưa đọc không tăng lại.
	if err := read(m[1]); err != nil {
		t.Fatal(err)
	}
	if _, _, n := e.state(id); n != 0 {
		t.Fatalf("đọc lùi về tin 2: chưa đọc %d, muốn vẫn 0", n)
	}
	if err := read(foreign); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("tin của hội thoại khác: lỗi %v", err)
	}
}

// [Task 167] Tin chào tự động có nút trả lời nhanh → quickReplies đúng thứ tự (tiêu đề, payload); tin không có nút
// (cột NULL) → rỗng.
func TestMessages_QuickReplies(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	u := e.user("nut@elimaz.test", e.pageA, "conversation.read")
	id := e.conv(conv{page: e.pageA, customer: e.customer("Khách nút", "")})
	base := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	plain := e.msg(msg{conv: id, page: e.pageA, ext: "mid-n1", at: base})
	var greet uuid.UUID
	e.scan(&greet, `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, direction, kind, origin,
		body, quick_replies, send_status, sent_at)
		VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'out', 'message', 'automation', 'Chào mình',
		        '[{"title":"Tư vấn size","payload":"SIZE"},{"title":"Xem mẫu mới","payload":"NEW"}]', 'sent', $3)
		RETURNING id`, id, e.pageA, base.Add(time.Second))
	p, err := e.messages(u, id, nil, nil, 10)
	if err != nil || len(p.Items) != 2 || p.Items[0].ID != plain || p.Items[1].ID != greet {
		t.Fatalf("trang: %v (lỗi %v)", msgIDs(p), err)
	}
	if len(p.Items[0].QuickReplies) != 0 {
		t.Fatalf("tin không nút: %v", p.Items[0].QuickReplies)
	}
	qr := p.Items[1].QuickReplies
	if len(qr) != 2 || qr[0].Title != "Tư vấn size" || qr[0].Payload != "SIZE" || qr[1].Title != "Xem mẫu mới" || qr[1].Payload != "NEW" {
		t.Fatalf("nút: %+v", qr)
	}
}
