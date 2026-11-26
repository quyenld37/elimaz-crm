package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

const tplPageAdmin = "qtv-page-a@elimaz.test"

// tplAPIEnv: môi trường kênh (newChAPIEnv: owner mọi quyền, sale-a đọc + trả lời Page A, doc-het chỉ đọc mọi Page) cộng
// người quản trị Page A (đọc + cài đặt hộp chat chỉ Page A) và hội thoại conv của khách "Lan" trên Page A.
type tplAPIEnv struct {
	*chAPIEnv
	conv uuid.UUID
}

func newTplAPIEnv(t *testing.T) *tplAPIEnv {
	t.Helper()
	e := &tplAPIEnv{chAPIEnv: newChAPIEnv(t)}
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, tplPageAdmin, "active"),
		[]string{"conversation.read", "conversation.settings"}, "page", e.pageA.String())
	var cust uuid.UUID
	if err := tenancy.WithTenant(t.Context(), e.staff.Owner, e.tenant, func(tx pgx.Tx) (err error) {
		cust, err = customer.Create(t.Context(), tx, "Lan")
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("tạo khách: %v", err)
	}
	e.conv = e.staff.Conversation(t, e.tenant, e.pageA, cust, "psid-1")
	return e
}

// Ghi chú qua API: sale-a → 201 tin note/out/staff kèm tên người ghi; jobs không đổi; gửi lại cùng khóa → bản lưu, không
// thêm tin; chỉ đọc (doc-het, có conversation.read mọi Page nhưng không reply) → 403 ở guard; Page không có quyền (thẻ
// sale-a, hội thoại Page B) → 404; thân rỗng → 400.
func TestNotesAPI_AddNote(t *testing.T) {
	t.Parallel()
	e := newTplAPIEnv(t)
	path := "/api/v1/conversations/" + e.conv.String() + "/notes"
	jobs := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs`)
	rec := serveAs(e.srv, e.keys, http.MethodPost, path, chReaderA, "n1", `{"text":"Khách hẹn mai chuyển khoản"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("ghi chú: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeJSON(t, rec)
	if got["kind"] != "note" || got["direction"] != "out" || got["origin"] != "staff" || got["body"] != "Khách hẹn mai chuyển khoản" ||
		got["senderName"] != chReaderA || got["sendStatus"] != nil {
		t.Fatalf("tin = %v", got)
	}
	again := serveAs(e.srv, e.keys, http.MethodPost, path, chReaderA, "n1", `{"text":"Khách hẹn mai chuyển khoản"}`)
	if again.Code != http.StatusCreated || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("gửi lại: %d %v", again.Code, again.Header())
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE kind = 'note'`); n != 1 {
		t.Fatalf("tin note = %d", n)
	}
	if after := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs`); after != jobs {
		t.Fatalf("jobs %d → %d", jobs, after)
	}
	expect(t, "chỉ đọc", serveAs(e.srv, e.keys, http.MethodPost, path, chManager, "n2", `{"text":"x"}`), http.StatusForbidden, "forbidden")
	var convB uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO conversation.conversations
			(tenant_id, channel_account_id, customer_id, kind, external_thread_ref, last_message_at)
			SELECT tenant_id, $1, customer_id, 'inbox', 'psid-b', now() FROM conversation.conversations WHERE id = $2
			RETURNING id`, e.pageB, e.conv).Scan(&convB)
	})
	expect(t, "Page B", serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/conversations/"+convB.String()+"/notes", chReaderA,
		"n3", `{"text":"x"}`), http.StatusNotFound, "not_found")
	expect(t, "thân rỗng", serveAs(e.srv, e.keys, http.MethodPost, path, chReaderA, "n4", `{"text":"   "}`),
		http.StatusBadRequest, "invalid_input")
}

// Quyết định chủ shop 26/11 (hai ca): người có conversation.settings chỉ trên Page A tạo mẫu chung → 403, tạo mẫu
// Page A → 201, sửa mẫu chung của owner → 403; owner (phạm vi all) tạo mẫu chung → 201. Thiếu conversation.settings
// (sale-a) → 403 ở guard. Phím tắt trùng → 409; biến lạ → 422; version cũ → 409; xóa → 204, lần hai → 404.
func TestReplyTemplatesAPI_PermissionsAndCRUD(t *testing.T) {
	t.Parallel()
	e := newTplAPIEnv(t)
	const base = "/api/v1/reply-templates"
	common := `{"shortcut":"xindiachi","title":"Xin địa chỉ","body":"Chào {ten_khach}, cho shop xin địa chỉ nhận hàng ạ"}`
	expect(t, "Page A tạo mẫu chung", serveAs(e.srv, e.keys, http.MethodPost, base, tplPageAdmin, "t1", common),
		http.StatusForbidden, "forbidden")
	expect(t, "sale không có settings", serveAs(e.srv, e.keys, http.MethodPost, base, chReaderA, "t2", common),
		http.StatusForbidden, "forbidden")
	rec := serveAs(e.srv, e.keys, http.MethodPost, base, chOwner, "t3", common)
	if rec.Code != http.StatusCreated {
		t.Fatalf("owner tạo mẫu chung: %d %s", rec.Code, rec.Body.String())
	}
	tpl := decodeJSON(t, rec)
	if tpl["pageId"] != nil || tpl["version"] != float64(1) || tpl["shortcut"] != "xindiachi" {
		t.Fatalf("mẫu = %v", tpl)
	}
	id, _ := tpl["id"].(string)
	pageTpl := `{"pageId":"` + e.pageA.String() + `","shortcut":"xinsdt","title":"Xin SĐT","body":"Cho shop xin SĐT ạ"}`
	if rec := serveAs(e.srv, e.keys, http.MethodPost, base, tplPageAdmin, "t4", pageTpl); rec.Code != http.StatusCreated {
		t.Fatalf("Page A tạo mẫu Page A: %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "Page A sửa mẫu chung", serveAs(e.srv, e.keys, http.MethodPatch, base+"/"+id, tplPageAdmin, "t5",
		`{"title":"Đổi","expectedVersion":1}`), http.StatusForbidden, "forbidden")
	expect(t, "Page A xóa mẫu chung", serveAs(e.srv, e.keys, http.MethodDelete, base+"/"+id, tplPageAdmin, "t6", ""),
		http.StatusForbidden, "forbidden")
	expect(t, "phím tắt trùng", serveAs(e.srv, e.keys, http.MethodPost, base, chOwner, "t7", common),
		http.StatusConflict, "already_exists")
	expect(t, "biến lạ", serveAs(e.srv, e.keys, http.MethodPost, base, chOwner, "t8",
		`{"shortcut":"abc","title":"A","body":"Chào {abc}"}`), http.StatusUnprocessableEntity, "invalid_input")
	rec = serveAs(e.srv, e.keys, http.MethodPatch, base+"/"+id, chOwner, "t9", `{"title":"Xin địa chỉ giao","expectedVersion":1}`)
	if got := decodeJSON(t, rec); rec.Code != http.StatusOK || got["version"] != float64(2) || got["title"] != "Xin địa chỉ giao" ||
		got["body"] != "Chào {ten_khach}, cho shop xin địa chỉ nhận hàng ạ" {
		t.Fatalf("sửa: %d %v", rec.Code, got)
	}
	expect(t, "version cũ", serveAs(e.srv, e.keys, http.MethodPatch, base+"/"+id, chOwner, "t10",
		`{"title":"X","expectedVersion":1}`), http.StatusConflict, "version_conflict")
	if rec := serveAs(e.srv, e.keys, http.MethodDelete, base+"/"+id, chOwner, "t11", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("xóa: %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "xóa lần hai", serveAs(e.srv, e.keys, http.MethodDelete, base+"/"+id, chOwner, "t12", ""),
		http.StatusNotFound, "not_found")
}

// Danh sách và hiển thị: q = "xin" ở Page A có mẫu chung và mẫu Page A, không có mẫu Page B; hiển thị cho khách Lan →
// "Chào Lan, …"; {ma_don} khi khách chưa có đơn → 422 template_variable_missing, name = ma_don; thẻ không đọc được Page
// của hội thoại → 404; pageId không đọc được → 404.
func TestReplyTemplatesAPI_ListAndRender(t *testing.T) {
	t.Parallel()
	e := newTplAPIEnv(t)
	const base = "/api/v1/reply-templates"
	for i, body := range []string{
		`{"shortcut":"xindiachi","title":"Xin địa chỉ","body":"Chào {ten_khach}, cho shop xin địa chỉ nhận hàng ạ"}`,
		`{"shortcut":"don","title":"Mã đơn","body":"Đơn {ma_don} đã lên"}`,
		`{"pageId":"` + e.pageA.String() + `","shortcut":"xinsdt","title":"Xin SĐT","body":"Cho xin SĐT"}`,
		`{"pageId":"` + e.pageB.String() + `","shortcut":"xinsize","title":"Xin size","body":"Size gì ạ"}`,
	} {
		if rec := serveAs(e.srv, e.keys, http.MethodPost, base, chOwner, "c"+string(rune('0'+i)), body); rec.Code != http.StatusCreated {
			t.Fatalf("tạo mẫu %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	rec := serveAs(e.srv, e.keys, http.MethodGet, base+"?q=xin&pageId="+e.pageA.String(), chReaderA, "", "")
	items, _ := decodeJSON(t, rec)["items"].([]any)
	var got []string
	ids := map[string]string{}
	for _, it := range items {
		m, _ := it.(map[string]any)
		s, _ := m["shortcut"].(string)
		got = append(got, s)
		ids[s], _ = m["id"].(string)
	}
	if rec.Code != http.StatusOK || len(got) != 2 || got[0] != "xindiachi" || got[1] != "xinsdt" {
		t.Fatalf("danh sách: %d %v", rec.Code, got)
	}
	expect(t, "pageId không đọc được", serveAs(e.srv, e.keys, http.MethodGet, base+"?pageId="+e.pageB.String(), chReaderA, "", ""),
		http.StatusNotFound, "not_found")
	all := serveAs(e.srv, e.keys, http.MethodGet, base, chOwner, "", "")
	allItems, _ := decodeJSON(t, all)["items"].([]any)
	for _, it := range allItems {
		m, _ := it.(map[string]any)
		s, _ := m["shortcut"].(string)
		ids[s], _ = m["id"].(string)
	}
	render := func(shortcut, email, key string) (int, map[string]any) {
		rec := serveAs(e.srv, e.keys, http.MethodPost, base+"/"+ids[shortcut]+"/render", email, key,
			`{"conversationId":"`+e.conv.String()+`"}`)
		return rec.Code, decodeJSON(t, rec)
	}
	if code, body := render("xindiachi", chReaderA, "r1"); code != http.StatusOK || body["text"] != "Chào Lan, cho shop xin địa chỉ nhận hàng ạ" {
		t.Fatalf("hiển thị: %d %v", code, body)
	}
	if code, body := render("don", chReaderA, "r2"); code != http.StatusUnprocessableEntity ||
		body["error"] != "template_variable_missing" || body["name"] != "ma_don" {
		t.Fatalf("thiếu mã đơn: %d %v", code, body)
	}
	if code, body := render("xinsize", chOwner, "r3"); code != http.StatusNotFound {
		t.Fatalf("mẫu Page B cho hội thoại Page A: %d %v", code, body)
	}
	if code, body := render("xindiachi", chReaderNo, "r4"); code != http.StatusNotFound || body["error"] != "not_found" {
		t.Fatalf("không có quyền trên Page: %d %v", code, body)
	}
}
