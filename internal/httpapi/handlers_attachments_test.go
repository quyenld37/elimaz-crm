package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/testkit"
)

// attEnv là môi trường test đường dẫn tệp đính kèm: tenant A, Page pageExt, người reader đọc Page đó, người other
// đọc một Page khác, kho tệp trong bộ nhớ.
type attEnv struct {
	staff  *testkit.Staff
	keys   *cfaccesstest.AccessKeys
	mem    *files.MemStore
	tenant uuid.UUID
	page   uuid.UUID
}

func newAttEnv(t *testing.T, pageExt, reader, other string) attEnv {
	t.Helper()
	conversation.RegisterFilePurposes()
	e := attEnv{staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t), mem: filestest.MemFileStore(t)}
	e.tenant = e.staff.Tenant(t, "A")
	e.page = e.staff.ChannelAccount(t, e.tenant, "facebook", pageExt, "Page A")
	for email, scope := range map[string]string{reader: e.page.String(), other: uuid.NewString()} {
		id := e.staff.User(t, e.tenant, email, "active")
		e.staff.Grant(t, e.tenant, id, []string{"conversation.read"}, "page", scope)
	}
	return e
}

// getter trả hàm gọi GET …/attachments/{f}/url của hội thoại c bằng người email (mã, thân JSON).
func (e attEnv) getter(t *testing.T) func(email string, c, f uuid.UUID) (int, map[string]any) {
	t.Helper()
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Files = e.mem
	srv := NewServer(d)
	return func(email string, c, f uuid.UUID) (int, map[string]any) {
		rec := serveAs(srv, e.keys, http.MethodGet, "/api/v1/conversations/"+c.String()+"/attachments/"+f.String()+"/url", email, "", "")
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}
}

// GET …/attachments/{fileId}/url (Task 155; TP02, TP04): ảnh ready → 200, đường dẫn trên gốc của kho tệp tới bucket
// private, không attachment, hạn ≤ 5 phút; tệp không phải ảnh → 200 có attachment (tên tep-…); ảnh chưa xử lý → 422
// attachment_not_ready; ảnh bị từ chối → 422 attachment_rejected; nhân viên chỉ có quyền Page khác, tệp không thuộc
// hội thoại, hội thoại không có → 404.
func TestGetAttachmentURL(t *testing.T) {
	t.Parallel()
	e := newAttEnv(t, "page-att", "sale-att@elimaz.test", "khac-att@elimaz.test")
	tenant, mem := e.tenant, e.mem
	ids := seedAttachments(t, e.staff, mem, tenant, e.page)
	conv, img, pending, rejected, doc, stray := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	get := e.getter(t)
	base, _, _ := strings.Cut(mustPresign(t, mem), "/tmp/x")
	before := time.Now()
	code, body := get("sale-att@elimaz.test", conv, img)
	if code != http.StatusOK {
		t.Fatalf("ảnh: %d %v", code, body)
	}
	u, _ := url.Parse(fmt.Sprint(body["url"]))
	exp, _ := time.Parse(time.RFC3339, fmt.Sprint(body["expiresAt"]))
	if !strings.HasPrefix(u.String(), base+"/private/priv/"+tenant.String()+"/"+img.String()+"/") || u.Query().Get("cd") != "" ||
		exp.Sub(before) > 5*time.Minute+time.Second || exp.Before(before) {
		t.Fatalf("ảnh: %v hạn %v", u, exp)
	}
	code, body = get("sale-att@elimaz.test", conv, doc)
	if code != http.StatusOK || !strings.Contains(fmt.Sprint(body["url"]), "attachment") ||
		!strings.Contains(fmt.Sprint(body["url"]), "tep-abcdef12.pdf") {
		t.Fatalf("tệp tải về: %d %v", code, body)
	}
	for _, c := range []struct {
		email     string
		conv, f   uuid.UUID
		code      int
		errorCode string
	}{
		{"sale-att@elimaz.test", conv, pending, http.StatusUnprocessableEntity, "attachment_not_ready"},
		{"sale-att@elimaz.test", conv, rejected, http.StatusUnprocessableEntity, "attachment_rejected"},
		{"khac-att@elimaz.test", conv, img, http.StatusNotFound, "not_found"},
		{"sale-att@elimaz.test", conv, stray, http.StatusNotFound, "not_found"},
		{"sale-att@elimaz.test", uuid.New(), img, http.StatusNotFound, "not_found"},
	} {
		if code, body := get(c.email, c.conv, c.f); code != c.code || body["error"] != c.errorCode {
			t.Errorf("%s %s: %d %v, muốn %d %s", c.email, c.f, code, body, c.code, c.errorCode)
		}
	}
}

// seedImages tạo ba ảnh chat_image (ready, uploaded, rejected) và một ảnh ready không gắn tin nào.
func seedImages(t *testing.T, staff *testkit.Staff, tenant uuid.UUID) (img, pending, rejected, stray uuid.UUID) {
	t.Helper()
	staff.Exec(t, tenant, func(tx pgx.Tx) error {
		img, stray = filestest.Image(t, tx, tenant, conversation.PurposeChatImage), filestest.Image(t, tx, tenant, conversation.PurposeChatImage)
		pending = filestest.Uploaded(t, tx, tenant, conversation.PurposeChatImage)
		rejected = filestest.Rejected(t, tx, tenant, conversation.PurposeChatImage)
		return nil
	})
	return img, pending, rejected, stray
}

// seedAttachments: hội thoại của khách trên page có một tin mang ba ảnh (ready, chưa xử lý, bị từ chối) và một PDF
// chat_attachment (ready, đối tượng trên kho); trả [hội thoại, ảnh, chưa xử lý, từ chối, PDF, ảnh không thuộc tin].
func seedAttachments(t *testing.T, staff *testkit.Staff, mem *files.MemStore, tenant, page uuid.UUID) [6]uuid.UUID {
	t.Helper()
	img, pending, rejected, stray := seedImages(t, staff, tenant)
	ctx := t.Context()
	var conv uuid.UUID
	doc := uuid.Must(uuid.NewV7())
	staff.Exec(t, tenant, func(tx pgx.Tx) error {
		key := files.ServerKey(tenant, doc)
		if err := mem.Put(ctx, files.BucketPrivate, key, bytes.NewReader([]byte("%PDF-1.4")), 8, "application/octet-stream"); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := files.Register(ctx, tx, files.NewFile{
			ID: doc, Purpose: conversation.PurposeChatAttachment, Key: key, Size: 8, ContentType: "application/pdf",
			FileName: "tep-abcdef12.pdf",
		}); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, `WITH k AS (INSERT INTO customer.customers (tenant_id, full_name, name_search)
			VALUES (current_setting('app.tenant_id')::uuid, 'Chị Mai', 'chi mai') RETURNING id, tenant_id)
			INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind, external_thread_ref,
			last_message_at) SELECT k.tenant_id, $1, k.id, 'inbox', 'psid-att', now() FROM k RETURNING id`, page).Scan(&conv); err != nil {
			return err //nolint:wrapcheck // test
		}
		atts := fmt.Sprintf(`[{"index":0,"type":"image","status":"stored","fileId":%q},
			{"index":1,"type":"image","status":"stored","fileId":%q},{"index":2,"type":"image","status":"stored","fileId":%q},
			{"index":3,"type":"file","status":"stored","fileId":%q,"downloadOnly":true}]`, img, pending, rejected, doc)
		_, err := tx.Exec(ctx, `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id,
			external_id, direction, kind, origin, attachments, sent_at) VALUES (current_setting('app.tenant_id')::uuid,
			$1, $2, 'm-att', 'in', 'message', 'customer', $3, now())`, conv, page, atts)
		return err //nolint:wrapcheck // test
	})
	return [6]uuid.UUID{conv, img, pending, rejected, doc, stray}
}

func mustPresign(t *testing.T, m *files.MemStore) string {
	t.Helper()
	u, err := m.PresignGet(t.Context(), files.BucketTmp, "x", time.Minute, "")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// [Task 501] Ảnh bài (chat_post_image, posts.image_file_id) qua getAttachmentUrl của hội thoại comment trên bài: người
// đọc Page → 200, đường tới đối tượng ở bucket private, mở trong trình duyệt (không attachment); cùng tệp xin qua hội
// thoại comment trên bài khác, qua hộp thư, hay bởi người chỉ đọc Page khác → 404.
func TestGetAttachmentURL_PostImage(t *testing.T) {
	t.Parallel()
	e := newAttEnv(t, "page-post-img", "sale-pi@elimaz.test", "khac-pi@elimaz.test")
	staff, mem, tenant, page := e.staff, e.mem, e.tenant, e.page
	ctx := t.Context()
	img := uuid.Must(uuid.NewV7())
	var onPost, otherPost, inbox uuid.UUID
	staff.Exec(t, tenant, func(tx pgx.Tx) error {
		key := files.ServerKey(tenant, img)
		png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...)
		if err := mem.Put(ctx, files.BucketPrivate, key, bytes.NewReader(png), int64(len(png)), "image/png"); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := files.Register(ctx, tx, files.NewFile{
			ID: img, Purpose: conversation.PurposeChatPostImage, Key: key, Size: int64(len(png)), ContentType: "image/png",
			FileName: "bai-abcdef12.png",
		}); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, `WITH k AS (INSERT INTO customer.customers (tenant_id, full_name, name_search)
				VALUES (current_setting('app.tenant_id')::uuid, 'Chị Lan', 'chi lan') RETURNING id, tenant_id),
			p1 AS (INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id, kind, image_file_id)
				VALUES (current_setting('app.tenant_id')::uuid, $1, 'post-anh', 'post', $2) RETURNING id),
			p2 AS (INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id, kind)
				VALUES (current_setting('app.tenant_id')::uuid, $1, 'post-khac', 'post') RETURNING id),
			c1 AS (INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind,
				external_thread_ref, post_id, last_message_at)
				SELECT k.tenant_id, $1, k.id, 'comment', 'post-anh:u1', p1.id, now() FROM k, p1 RETURNING id),
			c2 AS (INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind,
				external_thread_ref, post_id, last_message_at)
				SELECT k.tenant_id, $1, k.id, 'comment', 'post-khac:u1', p2.id, now() FROM k, p2 RETURNING id),
			c3 AS (INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind,
				external_thread_ref, last_message_at)
				SELECT k.tenant_id, $1, k.id, 'inbox', 'u1', now() FROM k RETURNING id)
			SELECT c1.id, c2.id, c3.id FROM c1, c2, c3`, page, img).Scan(&onPost, &otherPost, &inbox)
	})
	getFile := e.getter(t)
	get := func(email string, c uuid.UUID) (int, map[string]any) { return getFile(email, c, img) }
	base, _, _ := strings.Cut(mustPresign(t, mem), "/tmp/x")
	code, body := get("sale-pi@elimaz.test", onPost)
	u, _ := url.Parse(fmt.Sprint(body["url"]))
	if code != http.StatusOK || !strings.HasPrefix(u.String(), base+"/private/"+files.ServerKey(tenant, img)) ||
		u.Query().Get("cd") != "" || strings.Contains(u.String(), "attachment") {
		t.Fatalf("ảnh bài: %d %v", code, body)
	}
	for name, c := range map[string]struct {
		email string
		conv  uuid.UUID
	}{
		"comment bài khác":    {"sale-pi@elimaz.test", otherPost},
		"hộp thư":             {"sale-pi@elimaz.test", inbox},
		"người đọc Page khác": {"khac-pi@elimaz.test", onPost},
	} {
		if code, body := get(c.email, c.conv); code != http.StatusNotFound || body["error"] != "not_found" {
			t.Errorf("%s: %d %v, muốn 404 not_found", name, code, body)
		}
	}
}
