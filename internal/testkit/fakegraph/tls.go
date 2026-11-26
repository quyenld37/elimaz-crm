package fakegraph

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"time"
)

// defaultHosts là tên máy có trong chứng chỉ của máy giả: Graph, máy chủ tệp của Meta (ca CDN có vùng như
// scontent.fsgn5-1.fbcdn.net), và một máy lạ để test chặn theo tên.
func defaultHosts() []string {
	return []string{
		"localhost", "graph.facebook.com", "streaming-graph.facebook.com", "*.facebook.com", "facebook.com",
		"fbcdn.net", "*.fbcdn.net", "*.fsgn5-1.fbcdn.net", "*.xx.fbcdn.net", "fbsbx.com", "*.fbsbx.com",
		"evil.example",
	}
}

// selfSigned tạo chứng chỉ tự ký (ECDSA P-256, hạn 24 giờ) cho hosts và 127.0.0.1, ::1, cùng gốc tin cậy chứa nó.
func selfSigned(hosts []string) (tls.Certificate, *x509.CertPool, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("tạo khóa: %w", err)
	}
	now := time.Now()
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()),
		Subject:      pkix.Name{CommonName: "fakegraph"},
		NotBefore:    now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true,
		DNSNames:    hosts,
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("ký chứng chỉ: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("đọc chứng chỉ: %w", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool, nil
}
