package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// webhookTimeout là hạn gửi một webhook.
const webhookTimeout = 5 * time.Second

type advanceReq struct {
	To string `json:"to"`
}

// advance (POST /admin/parcels/{tracking}/advance {to: picked_up|delivered|created}): đổi trạng thái kiện rồi gửi
// webhook có chữ ký. Chế độ forge: không đổi trạng thái, webhook nói "delivered" (API vẫn trả trạng thái thật).
// Trả {parcel, webhookStatus} (0: không gửi được hoặc không cấu hình URL).
func (s *Server) advance(w http.ResponseWriter, r *http.Request) {
	var in advanceReq
	if err := decode(r, &in); err != nil || (in.To != rawCreated && in.To != rawPickedUp && in.To != rawDelivered) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_status"})
		return
	}
	tracking := r.PathValue("tracking")
	var (
		p   parcel
		ok  bool
		err error
	)
	claim := in.To
	if s.st.mode() == modeForge {
		p, ok = s.st.byTracking(tracking)
		claim = rawDelivered
	} else {
		p, ok, err = s.st.advance(tracking, in.To, s.now())
	}
	switch {
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	case !ok:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	status := s.sendWebhook(r.Context(), p, claim)
	writeJSON(w, http.StatusOK, map[string]any{"parcel": p, "webhookStatus": status})
}

// sendWebhook gửi một gói {"events":[…]} có chữ ký x-fake-signature tới FAKECARRIER_WEBHOOK_URL; trả mã HTTP (0 khi
// không gửi).
func (s *Server) sendWebhook(ctx context.Context, p parcel, claim string) int {
	if s.cfg.WebhookURL == "" {
		return 0
	}
	body, err := json.Marshal(fake.Packet{Events: []fake.Event{{
		EventID: newEventID(), AccountKey: p.AccountKey, Tracking: p.Tracking, Status: claim, SentAt: s.now().UTC(),
	}}})
	if err != nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(fake.SignatureHeader, fake.Sign(body, s.cfg.Secret))
	resp, err := s.hc.Do(req)
	if err != nil {
		s.logger.Warn("gửi webhook", logging.Err(err))
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func newEventID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "fc-" + hex.EncodeToString(b)
}

type modeReq struct {
	Mode string `json:"mode"`
}

// setMode (POST /admin/mode {mode}): ok, timeout_after_create, fail_before_create, forge.
func (s *Server) setMode(w http.ResponseWriter, r *http.Request) {
	var in modeReq
	if err := decode(r, &in); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_mode"})
		return
	}
	if err := s.st.setMode(in.Mode); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_mode"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"mode": in.Mode})
}
