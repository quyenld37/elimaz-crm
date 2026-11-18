package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	signingFile = "signing-key.pem" // khóa RSA ký thẻ (DEVACCESS_DATA)
	tlsKeyFile  = "tls-key.pem"     // khóa ECDSA của chứng chỉ (DEVACCESS_DATA)
	tlsCertFile = "tls-cert.pem"    // chứng chỉ (DEVACCESS_DATA)
	caFile      = "devaccess.pem"   // bản sao chứng chỉ công khai (DEVACCESS_CA_DIR)
	rsaBits     = 2048
	certTTL     = 5 * 365 * 24 * time.Hour
)

// keys là khóa ký thẻ và chứng chỉ HTTPS của devaccess.
type keys struct {
	signer  *rsa.PrivateKey
	kid     string
	tlsCert tls.Certificate
}

// loadKeys nạp khóa từ dataDir, thiếu thì sinh mới (lần chạy đầu) và ghi lại quyền 600; rồi chép chứng chỉ công khai
// vào caDir. Chứng chỉ tự ký (là CA của chính nó) cho host, localhost, 127.0.0.1.
func loadKeys(dataDir, caDir, host string, now time.Time) (*keys, error) {
	data, err := os.OpenRoot(dataDir)
	if err != nil {
		return nil, fmt.Errorf("mở thư mục dữ liệu: %w", err)
	}
	defer func() { _ = data.Close() }()
	signer, err := loadOrCreate(data, signingFile, func() (any, error) {
		return rsa.GenerateKey(rand.Reader, rsaBits)
	})
	if err != nil {
		return nil, err
	}
	rsaKey, ok := signer.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s không phải khóa RSA", signingFile) //nolint:err113 // lỗi một lần
	}
	tlsKey, err := loadOrCreate(data, tlsKeyFile, func() (any, error) {
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	})
	if err != nil {
		return nil, err
	}
	certPEM, err := data.ReadFile(tlsCertFile)
	if errors.Is(err, fs.ErrNotExist) {
		certPEM, err = selfSigned(tlsKey, host, now)
		if err == nil {
			err = data.WriteFile(tlsCertFile, certPEM, 0o600)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("chứng chỉ devaccess: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(tlsKey)
	if err != nil {
		return nil, fmt.Errorf("mã hóa khóa TLS: %w", err)
	}
	pair, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		return nil, fmt.Errorf("ghép chứng chỉ và khóa TLS: %w", err)
	}
	// 0644: api (người dùng khác trong container khác) đọc qua volume chỉ đọc; chỉ là chứng chỉ công khai.
	ca, err := os.OpenRoot(caDir)
	if err != nil {
		return nil, fmt.Errorf("mở thư mục CA: %w", err)
	}
	defer func() { _ = ca.Close() }()
	if err := ca.WriteFile(caFile, certPEM, 0o644); err != nil {
		return nil, fmt.Errorf("chép chứng chỉ công khai: %w", err)
	}
	sum := sha256.Sum256(rsaKey.N.Bytes())
	return &keys{signer: rsaKey, kid: "dev-" + hex.EncodeToString(sum[:8]), tlsCert: pair}, nil
}

// loadOrCreate đọc khóa PKCS#8 tệp name trong dir; chưa có thì sinh bằng gen và ghi (0600).
func loadOrCreate(dir *os.Root, path string, gen func() (any, error)) (any, error) {
	b, err := dir.ReadFile(path)
	if err == nil {
		blk, _ := pem.Decode(b)
		if blk == nil {
			return nil, fmt.Errorf("%s không phải PEM", path) //nolint:err113 // lỗi một lần
		}
		k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
		if err != nil {
			return nil, fmt.Errorf("đọc khóa %s: %w", path, err)
		}
		return k, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("đọc %s: %w", path, err)
	}
	k, err := gen()
	if err != nil {
		return nil, fmt.Errorf("sinh khóa %s: %w", path, err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return nil, fmt.Errorf("mã hóa khóa %s: %w", path, err)
	}
	if err := dir.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, fmt.Errorf("ghi khóa %s: %w", path, err)
	}
	return k, nil
}

func selfSigned(key any, host string, now time.Time) ([]byte, error) {
	signer, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s không phải khóa ECDSA", tlsKeyFile) //nolint:err113 // lỗi một lần
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return nil, fmt.Errorf("sinh số sê-ri: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "devaccess (chỉ máy dev)"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(certTTL),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{host, "localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &signer.PublicKey, signer)
	if err != nil {
		return nil, fmt.Errorf("ký chứng chỉ: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// jwk là một khóa công khai RSA trong bộ khóa của Cloudflare Access.
type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// jwks là bộ khóa theo dạng của Cloudflare Access ({"keys": [...]}), chỉ một khóa.
func (k *keys) jwks() []byte {
	enc := base64.RawURLEncoding
	pub := &k.signer.PublicKey
	set := struct {
		Keys []jwk `json:"keys"`
	}{Keys: []jwk{{
		Kid: k.kid, Kty: "RSA", Alg: "RS256", Use: "sig",
		N: enc.EncodeToString(pub.N.Bytes()), E: enc.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}}
	b, _ := json.Marshal(set) //nolint:errchkjson // chỉ chuỗi
	return b
}

// sign ký thẻ RS256 như Cloudflare Access: iss https://<host>:<tls_port>, aud, email, MFA trong trường cfg.mfaClaim.
func (k *keys) sign(cfg config, email string, now time.Time) (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"email":      email,
		"aud":        []string{cfg.aud},
		"iss":        cfg.issuer(),
		"iat":        now.Add(-time.Minute).Unix(),
		"exp":        now.Add(tokenTTL).Unix(),
		"sub":        "dev-" + email,
		cfg.mfaClaim: []string{"mfa"},
	})
	tok.Header["kid"] = k.kid
	s, err := tok.SignedString(k.signer)
	if err != nil {
		return "", fmt.Errorf("ký thẻ: %w", err)
	}
	return s, nil
}
