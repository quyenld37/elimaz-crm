package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/cfaccess"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// startAccess chạy máy chủ khóa giả trên 127.0.0.1 (TLS bằng chứng chỉ của CA riêng); trả bộ khóa và TeamDomain.
func startAccess(t *testing.T) (*accessKit, string) {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	team := ln.Addr().String()
	kit, err := newAccessKit(team, []string{"127.0.0.1", "localhost", "host.docker.internal"})
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: kit.handler(), TLSConfig: kit.tlsConfig(), ReadHeaderTimeout: time.Second}
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	return kit, team
}

func trustingClient(t *testing.T, caPEM []byte) *http.Client {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("ca.pem không đọc được")
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
}

// Thẻ của bộ thử phải qua đúng bộ kiểm thẻ thật của api (cfaccess) khi api tin CA của bộ thử; không tin CA thì
// không tải được khóa và thẻ bị từ chối.
func TestAccessTokenPassesRealVerifier(t *testing.T) {
	kit, team := startAccess(t)
	opts := cfaccess.Options{
		TeamDomain: team, AUD: "aud-lt", MFAClaim: "amr", MFAValues: []string{"mfa"},
		HTTPClient: trustingClient(t, kit.caPEM),
	}
	v, err := cfaccess.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := kit.sign("nv01@loadtest.invalid", "aud-lt", "amr", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	email, err := v.Verify(t.Context(), tok)
	if err != nil || email != "nv01@loadtest.invalid" {
		t.Fatalf("Verify = %q, %v", email, err)
	}

	opts.HTTPClient = &http.Client{Timeout: 2 * time.Second}
	untrusting, err := cfaccess.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := untrusting.Verify(t.Context(), tok); !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatalf("client không tin CA: muốn ErrInvalidToken, nhận %v", err)
	}
	if _, err := v.Verify(t.Context(), tok+"x"); !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatalf("thẻ sửa chữ ký: muốn ErrInvalidToken, nhận %v", err)
	}
}

func TestStaffEmail(t *testing.T) {
	for n, want := range map[int]string{1: "nv01@loadtest.invalid", 9: "nv09@loadtest.invalid", 50: "nv50@loadtest.invalid", 120: "nv120@loadtest.invalid"} {
		if got := staffEmail(n); got != want {
			t.Fatalf("staffEmail(%d) = %q, muốn %q", n, got, want)
		}
	}
}

// writeAccessFiles: ca.pem đọc được cho container (0644), tokens.json chỉ chủ đọc (0600), đủ n thẻ, email khác nhau.
func TestWriteAccessFiles(t *testing.T) {
	kit, _ := startAccess(t)
	dir := t.TempDir()
	if err := writeAccessFiles(dir, kit, 50, "aud-lt", "amr", 12*time.Hour); err != nil {
		t.Fatal(err)
	}
	ca, err := os.Stat(filepath.Join(dir, "ca.pem"))
	if err != nil || ca.Mode().Perm() != 0o644 {
		t.Fatalf("ca.pem: %v %v", ca, err)
	}
	st, err := os.Stat(filepath.Join(dir, "tokens.json"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("tokens.json: %v %v", st, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	var toks []staffToken
	if err := json.Unmarshal(b, &toks); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, tk := range toks {
		if tk.Token == "" || seen[tk.Email] {
			t.Fatalf("thẻ rỗng hoặc email trùng: %q", tk.Email)
		}
		seen[tk.Email] = true
	}
	if len(toks) != 50 {
		t.Fatalf("%d thẻ, muốn 50", len(toks))
	}
}
