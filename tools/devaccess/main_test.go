package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/cfaccess"
	"elimaz.com.vn/crm/internal/modules/identity"
)

const aud = "dev-aud"

var plainClient = &http.Client{Timeout: 5 * time.Second} //nolint:gochecknoglobals // client của test

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert // tcp
}

// devEnv là biến môi trường của một lần chạy: host 127.0.0.1 (chứng chỉ có IP này), hai cổng rảnh, hai thư mục riêng.
type devEnv struct {
	vars             map[string]string
	port, tlsPort    int
	dataDir, caDir   string
	teamDomain, base string
}

func newDevEnv(t *testing.T) *devEnv {
	t.Helper()
	d := &devEnv{port: freePort(t), tlsPort: freePort(t), dataDir: t.TempDir(), caDir: t.TempDir()}
	d.teamDomain = "127.0.0.1:" + strconv.Itoa(d.tlsPort)
	d.base = "http://127.0.0.1:" + strconv.Itoa(d.port)
	d.vars = map[string]string{
		"CRM_ENV": "dev", "PORT": strconv.Itoa(d.port), "TLS_PORT": strconv.Itoa(d.tlsPort),
		"DEVACCESS_HOST": "127.0.0.1", "ACCESS_AUD": aud, "DEVACCESS_DATA": d.dataDir, "DEVACCESS_CA_DIR": d.caDir,
	}
	return d
}

func (d *devEnv) getenv(k string) string { return d.vars[k] }

// start chạy công cụ tới khi test xong; chờ cổng phát thẻ trả /health 200.
func (d *devEnv) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, d.getenv, slog.New(slog.DiscardHandler)) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("run trả lỗi khi dừng: %v", err)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if code, _ := get(t, plainClient, d.base+"/health"); code == http.StatusOK {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("devaccess không lên trong 5 s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func get(t *testing.T, c *http.Client, u string) (int, string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)
	resp, err := c.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (d *devEnv) token(t *testing.T, email string) string {
	t.Helper()
	code, body := get(t, plainClient, d.base+"/token?email="+url.QueryEscape(email))
	if code != http.StatusOK {
		t.Fatalf("/token: %d %s", code, body)
	}
	return strings.TrimSpace(body)
}

// trustingCA là http.Client chỉ tin các chứng chỉ trong thư mục CA (như api với SSL_CERT_DIR).
func (d *devEnv) trustingCA(t *testing.T) *http.Client {
	t.Helper()
	pool := x509.NewCertPool()
	entries, err := os.ReadDir(d.caDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(d.caDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		pool.AppendCertsFromPEM(b)
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
}

// verifier là bộ kiểm thẻ thật của api, cấu hình như compose dev (amr = mfa).
func (d *devEnv) verifier(t *testing.T) *cfaccess.Verifier {
	t.Helper()
	v, err := cfaccess.New(cfaccess.Options{
		TeamDomain: d.teamDomain, AUD: aud, MFAClaim: "amr", MFAValues: []string{"mfa"}, HTTPClient: d.trustingCA(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestToken_AcceptedByRealVerifier(t *testing.T) {
	d := newDevEnv(t)
	d.start(t)
	email, err := d.verifier(t).Verify(t.Context(), d.token(t, "chu-shop@elimaz.dev"))
	if err != nil {
		t.Fatalf("bộ kiểm thẻ thật từ chối thẻ của devaccess: %v", err)
	}
	if email != "chu-shop@elimaz.dev" {
		t.Errorf("email %q", email)
	}
}

// Thẻ sai AUD (cấu hình api khác) bị từ chối: devaccess không làm yếu bộ kiểm.
func TestToken_WrongAUDRejected(t *testing.T) {
	d := newDevEnv(t)
	d.vars["ACCESS_AUD"] = "aud-khac"
	d.start(t)
	_, err := d.verifier(t).Verify(t.Context(), d.token(t, "a@b.c"))
	if !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatalf("thẻ sai AUD: %v, muốn ErrInvalidToken", err)
	}
}

// Khởi động lại (container dựng lại, volume giữ): cùng khóa ký, cùng chứng chỉ — api đã tin chứng chỉ cũ và còn giữ
// khóa cũ trong bộ nhớ vẫn kiểm được thẻ mới.
func TestRestart_KeepsKeysAndCert(t *testing.T) {
	d := newDevEnv(t)
	k1, err := loadKeys(d.dataDir, d.caDir, "127.0.0.1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ca1, err := os.ReadFile(filepath.Join(d.caDir, caFile))
	if err != nil {
		t.Fatal(err)
	}
	k2, err := loadKeys(d.dataDir, d.caDir, "127.0.0.1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ca2, _ := os.ReadFile(filepath.Join(d.caDir, caFile))
	if k1.kid != k2.kid || !k1.signer.Equal(k2.signer) || string(ca1) != string(ca2) {
		t.Fatal("lần chạy thứ hai sinh khóa hoặc chứng chỉ mới")
	}
}

// Thư mục CA (api gắn chỉ đọc) chỉ có chứng chỉ công khai; khóa riêng nằm trong thư mục dữ liệu, quyền 600.
func TestFiles_PrivateKeysStayPrivate(t *testing.T) {
	d := newDevEnv(t)
	if _, err := loadKeys(d.dataDir, d.caDir, "devaccess", time.Now()); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(d.caDir)
	if len(ents) != 1 {
		t.Fatalf("thư mục CA có %d tệp, muốn 1", len(ents))
	}
	ca, _ := os.ReadFile(filepath.Join(d.caDir, ents[0].Name()))
	if !strings.Contains(string(ca), "BEGIN CERTIFICATE") || strings.Contains(string(ca), "PRIVATE KEY") {
		t.Errorf("tệp CA phải chỉ có chứng chỉ:\n%s", ca)
	}
	ents, _ = os.ReadDir(d.dataDir)
	for _, e := range ents {
		fi, _ := e.Info()
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s quyền %v, muốn 600", e.Name(), fi.Mode().Perm())
		}
	}
	cert, err := x509.ParseCertificate(mustPEM(t, ca))
	if err != nil {
		t.Fatal(err)
	}
	if err := cert.VerifyHostname("devaccess"); err != nil {
		t.Errorf("chứng chỉ không có tên devaccess: %v", err)
	}
}

func TestTokenEndpoint_RequiresEmail(t *testing.T) {
	d := newDevEnv(t)
	d.start(t)
	for _, q := range []string{"", "?email=", "?email=khong-co-a-cong"} {
		if code, _ := get(t, plainClient, d.base+"/token"+q); code != http.StatusBadRequest {
			t.Errorf("/token%s: %d, muốn 400", q, code)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	c, err := loadConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if c.port != 4195 || c.tlsPort != 8443 || c.host != "devaccess" || c.aud != "dev-aud" || c.mfaClaim != "amr" ||
		c.dataDir != "/data" || c.caDir != "/ca" {
		t.Errorf("mặc định sai: %+v", c)
	}
	for _, tc := range []struct{ k, v string }{
		{"CRM_ENV", "production"}, {"PORT", "0"}, {"TLS_PORT", "abc"}, {"DEVACCESS_HOST", "a/b"},
	} {
		if _, err := loadConfig(func(k string) string {
			if k == tc.k {
				return tc.v
			}
			return ""
		}); err == nil {
			t.Errorf("%s=%q: muốn lỗi", tc.k, tc.v)
		}
	}
}

func mustPEM(t *testing.T, b []byte) []byte {
	t.Helper()
	blk, _ := pem.Decode(b)
	if blk == nil {
		t.Fatal("không đọc được PEM")
	}
	return blk.Bytes
}
