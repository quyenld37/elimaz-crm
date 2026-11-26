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

// GET …/attachments/{fileId}/url (Task 155; TP02, TP04): ảnh ready → 200, đường dẫn trên gốc của kho tệp tới bucket
// private, không attachment, hạn ≤ 5 phút; tệp không phải ảnh → 200 có attachment (tên tep-…); ảnh chưa xử lý → 422
// attachment_not_ready; ảnh bị từ chối → 422 attachment_rejected; nhân viên chỉ có quyền Page khác, tệp không thuộc
// hội thoại, hội thoại không có → 404.
func TestGetAttachmentURL(t *testing.T) {
	t.Parallel()
	conversation.RegisterFilePurposes()
	staff := testkit.OpenStaff(t)
	keys := cfaccesstest.AccessKeyServer(t)
	mem := filestest.MemFileStore(t)
	tenant := staff.Tenant(t, "A")
	page := staff.ChannelAccount(t, tenant, "facebook", "page-att", "Page A")
	for email, scope := range map[string]string{"sale-att@elimaz.test": page.String(), "khac-att@elimaz.test": uuid.NewString()} {
		id := staff.User(t, tenant, email, "active")
		staff.Grant(t, tenant, id, []string{"conversation.read"}, "page", scope)
	}
	ids := seedAttachments(t, staff, mem, tenant, page)
	conv, img, pending, rejected, doc, stray := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	d := testServerDeps(t, staff, keys, 0)
	d.Files = mem
	srv := NewServer(d)
	get := func(email string, c, f uuid.UUID) (int, map[string]any) {
		rec := serveAs(srv, keys, http.MethodGet, "/api/v1/conversations/"+c.String()+"/attachments/"+f.String()+"/url", email, "", "")
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}
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
