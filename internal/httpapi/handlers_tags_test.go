package httpapi

import (
	"net/http"
	"testing"

	"elimaz.com.vn/crm/internal/modules/conversation/convtest"
)

// Nhãn chung (Q7, chủ shop 26/11): conversation.settings phạm vi all tạo được (201); grant chỉ một Page → 403; tên trùng
// khác hoa thường → 409 already_exists; tên rỗng → 400/422; sửa version cũ → 409; gắn 204, gắn lại 204; sale gắn nhãn
// hội thoại Page 1 được, nhãn không có → 404; xóa mềm → 204, không còn trong danh sách; gắn nhãn đã xóa → 404.
func TestTagsAPI_Q7(t *testing.T) {
	t.Parallel()
	e := newAssignAPIEnv(t)
	create := `{"name":"Đã lên đơn","color":"green"}`
	expect(t, "cài đặt chỉ một Page", serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/conversation-tags", cvPageOnly, "t0", create),
		http.StatusForbidden, "forbidden")
	expect(t, "sale không có conversation.settings", serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/conversation-tags", cvAn, "t1", create),
		http.StatusForbidden, "forbidden")
	code, tag := e.do(http.MethodPost, "/api/v1/conversation-tags", cvManager, "t2", create)
	if code != http.StatusCreated || tag["name"] != "Đã lên đơn" || tag["version"] != float64(1) {
		t.Fatalf("tạo nhãn: %d %v", code, tag)
	}
	id, _ := tag["id"].(string)
	expect(t, "tên trùng", serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/conversation-tags", cvManager, "t3",
		`{"name":"ĐÃ LÊN ĐƠN","color":"red"}`), http.StatusConflict, "already_exists")
	expect(t, "màu sai", serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/conversation-tags", cvManager, "t4",
		`{"name":"Khác","color":"Đỏ"}`), http.StatusUnprocessableEntity, "invalid_input")
	expect(t, "sửa cài đặt chỉ một Page", serveAs(e.srv, e.keys, http.MethodPatch, "/api/v1/conversation-tags/"+id, cvPageOnly, "t5",
		`{"color":"red","expectedVersion":1}`), http.StatusForbidden, "forbidden")
	expect(t, "sửa version cũ", serveAs(e.srv, e.keys, http.MethodPatch, "/api/v1/conversation-tags/"+id, cvManager, "t6",
		`{"color":"red","expectedVersion":9}`), http.StatusConflict, "version_conflict")
	if code, got := e.do(http.MethodPatch, "/api/v1/conversation-tags/"+id, cvManager, "t7", `{"color":"red","expectedVersion":1}`); code != http.StatusOK ||
		got["color"] != "red" || got["version"] != float64(2) {
		t.Fatalf("sửa: %d %v", code, got)
	}
	c := convtest.Conversation(t, e.staff, e.tenant, e.page1, "psid-tag")
	link := "/api/v1/conversations/" + c.String() + "/tags/" + id
	for i, key := range []string{"l1", "l2"} {
		if rec := serveAs(e.srv, e.keys, http.MethodPut, link, cvAn, key, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("gắn lần %d: %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.conversation_tag_links WHERE conversation_id = $1`, c); n != 1 {
		t.Fatalf("dòng gắn = %d", n)
	}
	expect(t, "người ngoài Page gắn nhãn", serveAs(e.srv, e.keys, http.MethodPut, link, cvOutside, "l3", ""),
		http.StatusForbidden, "forbidden")
	expect(t, "nhãn không có", serveAs(e.srv, e.keys, http.MethodPut, "/api/v1/conversations/"+c.String()+"/tags/"+c.String(),
		cvAn, "l4", ""), http.StatusNotFound, "not_found")
	if rec := serveAs(e.srv, e.keys, http.MethodDelete, link, cvAn, "l5", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("gỡ: %d", rec.Code)
	}
	if rec := serveAs(e.srv, e.keys, http.MethodDelete, link, cvAn, "l6", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("gỡ lại: %d", rec.Code)
	}
	expect(t, "xóa cài đặt chỉ một Page", serveAs(e.srv, e.keys, http.MethodDelete, "/api/v1/conversation-tags/"+id, cvPageOnly, "d0", ""),
		http.StatusForbidden, "forbidden")
	if rec := serveAs(e.srv, e.keys, http.MethodDelete, "/api/v1/conversation-tags/"+id, cvManager, "d1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("xóa: %d %s", rec.Code, rec.Body.String())
	}
	if code, list := e.do(http.MethodGet, "/api/v1/conversation-tags", cvAn, "", ""); code != http.StatusOK || len(list["items"].([]any)) != 0 { //nolint:forcetypeassert // test
		t.Fatalf("danh sách sau khi xóa: %d %v", code, list)
	}
	expect(t, "gắn nhãn đã xóa", serveAs(e.srv, e.keys, http.MethodPut, link, cvAn, "l7", ""), http.StatusNotFound, "not_found")
	expect(t, "xóa lần hai", serveAs(e.srv, e.keys, http.MethodDelete, "/api/v1/conversation-tags/"+id, cvManager, "d2", ""),
		http.StatusNotFound, "not_found")
}
