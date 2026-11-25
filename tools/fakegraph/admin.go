package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

const maxAdminBody = 1 << 20

// adminReq là thân JSON của mọi đường /_fake/* (mỗi đường dùng vài trường).
type adminReq struct {
	Path          string          `json:"path"`
	HTTPStatus    int             `json:"httpStatus"`
	Code          int             `json:"code"`
	Subcode       int             `json:"subcode"`
	Message       string          `json:"message"`
	RegainMinutes int             `json:"regainMinutes"`
	Pct           int             `json:"pct"`
	Token         string          `json:"token"`
	VideoID       string          `json:"videoId"`
	ID            string          `json:"id"`
	FromID        string          `json:"fromId"`
	FromName      string          `json:"fromName"`
	PageID        string          `json:"pageId"`
	PSID          string          `json:"psid"`
	Target        string          `json:"target"`
	Payload       json.RawMessage `json:"payload"`
	Status        string          `json:"status"`
}

// mountAdmin gắn các đường điều khiển cho console dev và k6. Mỗi đường POST nhận adminReq; GET /_fake/calls trả các
// yêu cầu đã nhận (lọc theo ?path=).
func mountAdmin(mux *http.ServeMux, fg *fakegraph.Server) {
	post := func(pattern string, f func(r *http.Request, a adminReq) (any, error)) {
		mux.HandleFunc("POST "+pattern, func(w http.ResponseWriter, r *http.Request) {
			var a adminReq
			if err := json.NewDecoder(io.LimitReader(r.Body, maxAdminBody)).Decode(&a); err != nil {
				http.Error(w, "thân JSON hỏng", http.StatusBadRequest)
				return
			}
			out, err := f(r, a)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
		})
	}
	ok := map[string]bool{"ok": true}
	post("/_fake/fail-next", func(_ *http.Request, a adminReq) (any, error) {
		fg.FailNext(a.Path, fakegraph.GraphErrorSpec{
			HTTPStatus: a.HTTPStatus, Code: a.Code, Subcode: a.Subcode, Type: "OAuthException", Message: a.Message,
			RegainMinutes: a.RegainMinutes,
		})
		return ok, nil
	})
	post("/_fake/usage", func(_ *http.Request, a adminReq) (any, error) { fg.SetUsagePct(a.Pct); return ok, nil })
	post("/_fake/revoke", func(_ *http.Request, a adminReq) (any, error) { fg.RevokeToken(a.Token); return ok, nil })
	post("/_fake/live-video", func(_ *http.Request, a adminReq) (any, error) {
		fg.AddLiveVideo(fakegraph.LiveVideo{ID: a.VideoID, PageID: a.PageID, VideoID: a.VideoID, Status: a.Status})
		return ok, nil
	})
	post("/_fake/live-comment", func(_ *http.Request, a adminReq) (any, error) {
		fg.PushLiveComment(a.VideoID, fakegraph.Comment{ID: a.ID, FromID: a.FromID, FromName: a.FromName, Message: a.Message, Created: time.Now().UTC()})
		return ok, nil
	})
	post("/_fake/drop-stream", func(_ *http.Request, a adminReq) (any, error) { fg.DropStream(a.VideoID); return ok, nil })
	post("/_fake/inbound-message", func(_ *http.Request, a adminReq) (any, error) {
		fg.AddInboundMessage(a.PageID, a.PSID, a.ID, a.Message, time.Now().UTC())
		return ok, nil
	})
	post("/_fake/webhook", func(r *http.Request, a adminReq) (any, error) {
		status, err := fg.EmitWebhook(r.Context(), a.Target, a.Payload)
		if err != nil {
			return nil, fmt.Errorf("gửi webhook giả: %w", err)
		}
		return map[string]int{"status": status}, nil
	})
	mux.HandleFunc("GET /_fake/calls", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fg.Calls(r.URL.Query().Get("path")))
	})
}
