package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// CH18, CH19 qua HTTP: tin in_doubt gửi lại → 201 tin mới queued, mayDuplicate = true, tin cũ discarded, thêm một lệnh;
// gửi lại lần nữa tin cũ → 409 version_conflict; tin expired gửi lại → mayDuplicate = false; tin expired bỏ → 200
// discarded; tin đã sent bỏ → 409; tin không có → 404 (cả gửi lại lẫn bỏ).
func TestSendRecoveryAPI(t *testing.T) {
	t.Parallel()
	e := newSendAPIEnv(t)
	path := "/api/v1/conversations/" + e.convA.String() + "/messages"
	sendOne := func(text, status string) string {
		t.Helper()
		m := mustOK(t, "sendMessage", e.send(http.MethodPost, path, chatSaleA, `{"text":"`+text+`"}`), http.StatusCreated)
		id, _ := m["id"].(string)
		e.exec(`UPDATE conversation.messages SET send_status = $1 WHERE id = $2`, status, id)
		return id
	}

	doubt := sendOne("Tin không rõ", "in_doubt")
	got := mustOK(t, "resendMessage", e.send(http.MethodPost, "/api/v1/messages/"+doubt+"/resend", chatSaleA, ""), http.StatusCreated)
	msg, _ := got["message"].(map[string]any)
	if got["mayDuplicate"] != true || msg["sendStatus"] != "queued" || msg["body"] != "Tin không rõ" || msg["id"] == doubt ||
		msg["origin"] != "staff" {
		t.Fatalf("gửi lại tin in_doubt = %v", got)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE id = $1 AND send_status = 'discarded'`, doubt); n != 1 {
		t.Fatal("tin cũ chưa discarded")
	}
	if n := e.sendJobs(); n != 2 {
		t.Fatalf("%d lệnh gửi, muốn 2", n)
	}
	expect(t, "gửi lại lần hai", e.send(http.MethodPost, "/api/v1/messages/"+doubt+"/resend", chatSaleA, ""),
		http.StatusConflict, "version_conflict")

	late := sendOne("Tin quá hạn", "expired")
	got = mustOK(t, "resendMessage", e.send(http.MethodPost, "/api/v1/messages/"+late+"/resend", chatSaleA, ""), http.StatusCreated)
	if got["mayDuplicate"] != false {
		t.Fatalf("gửi lại tin expired = %v", got)
	}

	drop := sendOne("Tin bỏ", "expired")
	got = mustOK(t, "discardMessage", e.send(http.MethodPost, "/api/v1/messages/"+drop+"/discard", chatSaleA, ""), http.StatusOK)
	if got["sendStatus"] != "discarded" || got["id"] != drop || got["body"] != "Tin bỏ" {
		t.Fatalf("bỏ tin = %v", got)
	}
	sent := sendOne("Tin đã gửi", "sent")
	expect(t, "bỏ tin đã gửi", e.send(http.MethodPost, "/api/v1/messages/"+sent+"/discard", chatSaleA, ""),
		http.StatusConflict, "version_conflict")
	none := uuid.NewString()
	expect(t, "gửi lại tin không có", e.send(http.MethodPost, "/api/v1/messages/"+none+"/resend", chatSaleA, ""),
		http.StatusNotFound, "not_found")
	expect(t, "bỏ tin không có", e.send(http.MethodPost, "/api/v1/messages/"+none+"/discard", chatSaleA, ""),
		http.StatusNotFound, "not_found")
}
