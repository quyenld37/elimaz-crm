package fakegraph

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
)

// Sign trả giá trị header X-Hub-Signature-256 của payload: "sha256=" + hex(HMAC-SHA256(payload, app secret)).
func (s *Server) Sign(payload []byte) string {
	m := hmac.New(sha256.New, []byte(s.o.AppSecret))
	m.Write(payload)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// EmitWebhook gửi payload tới targetURL như Meta gửi webhook (POST JSON có X-Hub-Signature-256), trả mã trạng thái.
func (s *Server) EmitWebhook(ctx context.Context, targetURL string, payload []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("dựng webhook: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", s.Sign(payload))
	resp, err := s.hc.Do(req)
	if err != nil {
		return 0, fmt.Errorf("gửi webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
