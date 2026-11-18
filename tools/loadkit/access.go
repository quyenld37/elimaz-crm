package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Máy chủ khóa Cloudflare Access giả của bộ thử tải: một CA riêng sinh mỗi lần chạy, chứng chỉ TLS cho các tên máy
// mà container api gọi tới (host.docker.internal), một khóa RSA ký thẻ. Container api tin CA này qua SSL_CERT_FILE
// (chỉ trong loadtest/compose.loadtest.yml); mã api không đổi, thẻ đi qua đúng bộ kiểm cfaccess thật.

const (
	accessKid    = "loadtest-1"
	certsPath    = "/cdn-cgi/access/certs"
	certValidFor = 48 * time.Hour
	rsaBits      = 2048
	// staffDomain: tên miền .invalid (RFC 2606) cho email nhân viên giả, không bao giờ là địa chỉ thật.
	staffDomain = "loadtest.invalid"
)

type accessKit struct {
	teamDomain string
	key        *rsa.PrivateKey
	caPEM      []byte
	cert       tls.Certificate
}

// staffToken là một dòng của tokens.json.
type staffToken struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

// staffEmail trả email của nhân viên giả thứ n (nv01@loadtest.invalid…).
func staffEmail(n int) string { return fmt.Sprintf("nv%02d@%s", n, staffDomain) }

// newAccessKit sinh CA, chứng chỉ máy chủ cho hosts (tên hoặc IP) và khóa ký thẻ; teamDomain là host:port mà api
// đặt ở ACCESS_TEAM_DOMAIN (iss của thẻ là https://teamDomain).
func newAccessKit(teamDomain string, hosts []string) (*accessKit, error) {
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("sinh khóa CA: %w", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()), Subject: pkix.Name{CommonName: "CRM loadtest CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(certValidFor), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("ký CA: %w", err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, fmt.Errorf("đọc CA: %w", err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("sinh khóa chứng chỉ máy chủ: %w", err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano() + 1), Subject: pkix.Name{CommonName: "CRM loadtest Access"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(certValidFor),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			leafTmpl.IPAddresses = append(leafTmpl.IPAddresses, ip)
		} else {
			leafTmpl.DNSNames = append(leafTmpl.DNSNames, h)
		}
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("ký chứng chỉ máy chủ: %w", err)
	}
	signKey, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return nil, fmt.Errorf("sinh khóa ký thẻ: %w", err)
	}
	return &accessKit{
		teamDomain: teamDomain,
		key:        signKey,
		caPEM:      pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		cert:       tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: leafKey},
	}, nil
}

func (k *accessKit) tlsConfig() *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{k.cert}, MinVersion: tls.VersionTLS12}
}

// handler phục vụ bộ khóa công khai ở /cdn-cgi/access/certs theo dạng của Cloudflare Access.
func (k *accessKit) handler() http.Handler {
	b64 := base64.RawURLEncoding.EncodeToString
	body, err := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kid": accessKid, "kty": "RSA", "alg": "RS256", "use": "sig",
		"n": b64(k.key.N.Bytes()), "e": b64(big.NewInt(int64(k.key.E)).Bytes()),
	}}})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err != nil || r.URL.Path != certsPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}

// sign ký thẻ của email cho ứng dụng aud, có dấu bước hai mfaClaim = ["mfa"], hết hạn sau ttl.
func (k *accessKit) sign(email, aud, mfaClaim string, ttl time.Duration) (string, error) {
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"email": email, "aud": []string{aud}, "iss": "https://" + k.teamDomain, "sub": "loadtest-" + email,
		"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(ttl).Unix(), mfaClaim: []string{"mfa"},
	})
	tok.Header["kid"] = accessKid
	s, err := tok.SignedString(k.key)
	if err != nil {
		return "", fmt.Errorf("ký thẻ: %w", err)
	}
	return s, nil
}

// writeAccessFiles ghi dir/ca.pem (0644: container api đọc bằng uid khác) và dir/tokens.json (0600) với n thẻ.
func writeAccessFiles(dir string, k *accessKit, n int, aud, mfaClaim string, ttl time.Duration) error {
	toks := make([]staffToken, 0, n)
	for i := 1; i <= n; i++ {
		s, err := k.sign(staffEmail(i), aud, mfaClaim, ttl)
		if err != nil {
			return err
		}
		toks = append(toks, staffToken{Email: staffEmail(i), Token: s})
	}
	b, err := json.Marshal(toks)
	if err != nil {
		return fmt.Errorf("mã hóa tokens.json: %w", err)
	}
	if err := writeFileMode(filepath.Join(dir, "tokens.json"), b, 0o600); err != nil {
		return err
	}
	return writeFileMode(filepath.Join(dir, "ca.pem"), k.caPEM, 0o644)
}

// writeFileMode ghi tệp và đặt đúng quyền (umask không làm lệch).
func writeFileMode(path string, b []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, b, mode); err != nil { //nolint:gosec // thư mục -out do người chạy (run.sh) đưa vào
		return fmt.Errorf("ghi %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil { //nolint:gosec // như trên
		return fmt.Errorf("đặt quyền %s: %w", path, err)
	}
	return nil
}
