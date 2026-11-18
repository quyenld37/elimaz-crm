package e2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/testkit"
)

// TestIngestServesHooks chạy vai trò ingest thật: POST /hooks/selftest có chữ ký đúng → 200 và một việc tách gói;
// route của api không có trên ingest; /health của ingest trả số tệp tạm.
func TestIngestServesHooks(t *testing.T) {
	bin := buildBinary(t)
	_, _, dbURL := testkit.StartPostgres(t)
	port := freePort(t)
	env := append(roleEnv("ingest", dbURL, port), "SPOOL_DIR="+t.TempDir()) // biến sau cùng thắng
	_, _, stdout, stderr := startRole(t, bin, env)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := waitReady(ctx, base+"/health/ready"); err != nil {
		t.Fatalf("ingest chưa sẵn sàng: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	body := []byte(`{"events":[{"kind":"ping","eventId":"e2e","tenantKey":"k","sentAt":"2026-11-18T00:00:00Z"}]}`)
	m := hmac.New(sha256.New, []byte(secret32))
	m.Write(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/hooks/selftest", bytes.NewReader(body))
	req.Header.Set("X-Selftest-Signature", "sha256="+hex.EncodeToString(m.Sum(nil)))
	if status, got := call(t, req); status != http.StatusOK || strings.TrimSpace(got) != `{"received":true}` {
		t.Fatalf("POST /hooks/selftest = %d %q\nstderr:\n%s", status, got, stderr.String())
	}
	for _, p := range []string{"/api/v1/stream", "/api/v1/me"} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+p, nil)
		if status, _ := call(t, req); status != http.StatusNotFound {
			t.Fatalf("GET %s trên ingest = %d, muốn 404", p, status)
		}
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if status, got := call(t, req); status != http.StatusOK || !strings.Contains(got, `"spoolFiles":0`) {
		t.Fatalf("/health = %d %q", status, got)
	}
}

func call(t *testing.T, req *http.Request) (int, string) {
	t.Helper()
	status, _, b := testkit.Do(t, req)
	return status, string(b)
}
