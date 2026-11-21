// Package cfaccesstest dựng máy chủ khóa Cloudflare Access thử và thẻ ký bằng khóa đó cho test. Tách khỏi
// internal/testkit vì bộ kiểm thẻ cfaccess nhập identity (Task 424).
package cfaccesstest

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess"
)

// AccessMFAClaim là tên trường bước hai mặc định của thẻ thử (cấu hình thật: ACCESS_MFA_CLAIM).
const AccessMFAClaim = "amr"

// AccessClaims mô tả thẻ Cloudflare Access thử. Iss rỗng → https://<TeamDomain>; Exp là thời gian còn lại (âm: đã hết
// hạn; 0: 5 phút); MFA nil → không có trường bước hai; Extra ghi đè hoặc thêm trường bất kỳ (ví dụ MFA kiểu lạ).
type AccessClaims struct {
	Email string
	AUD   string
	Iss   string
	Exp   time.Duration
	MFA   []string
	Extra map[string]any
}

// AccessKeys là máy chủ khóa thử của Cloudflare Access (TLS, phục vụ /cdn-cgi/access/certs) cùng khóa ký thẻ.
type AccessKeys struct {
	// TeamDomain là host:port của máy chủ thử, dùng như ACCESS_TEAM_DOMAIN.
	TeamDomain string
	// Client tin chứng chỉ của máy chủ thử; truyền vào bộ kiểm thẻ.
	Client *http.Client

	t       testing.TB
	mu      sync.Mutex
	kid     string
	key     *rsa.PrivateKey
	serial  int
	fetches atomic.Int64
	failing atomic.Bool
}

// AccessKeyServer dựng máy chủ khóa thử với một khóa RSA 2048 bit; đóng khi test xong.
func AccessKeyServer(t testing.TB) *AccessKeys {
	t.Helper()
	k := &AccessKeys{t: t}
	k.Rotate()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cdn-cgi/access/certs" {
			http.NotFound(w, r)
			return
		}
		k.fetches.Add(1)
		if k.failing.Load() {
			http.Error(w, "loi thu", http.StatusInternalServerError)
			return
		}
		k.mu.Lock()
		body := jwksJSON(k.kid, &k.key.PublicKey)
		k.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	k.TeamDomain = u.Host
	k.Client = srv.Client()
	return k
}

// Verifier dựng bộ kiểm thẻ thật (cfaccess) trỏ tới máy chủ khóa thử: AUD aud, bước hai AccessMFAClaim với giá trị
// "mfa" (thẻ Sign có MFA: []string{"mfa"} qua được).
func (k *AccessKeys) Verifier(t testing.TB, aud string) *cfaccess.Verifier {
	t.Helper()
	v, err := cfaccess.New(cfaccess.Options{
		TeamDomain: k.TeamDomain, AUD: aud, MFAClaim: AccessMFAClaim, MFAValues: []string{"mfa"}, HTTPClient: k.Client,
	})
	if err != nil {
		t.Fatalf("testkit: dựng bộ kiểm thẻ Access: %v", err)
	}
	return v
}

// Rotate thay khóa đang phục vụ bằng khóa mới có kid mới; trả kid mới.
func (k *AccessKeys) Rotate() string {
	key := newRSAKey(k.t)
	k.mu.Lock()
	defer k.mu.Unlock()
	k.serial++
	k.kid = "kid-" + strconv.Itoa(k.serial)
	k.key = key
	return k.kid
}

// SetFailing bật, tắt chế độ máy chủ khóa trả 500 (vẫn đếm vào Fetches).
func (k *AccessKeys) SetFailing(on bool) { k.failing.Store(on) }

// Fetches là số lần máy chủ thử đã phục vụ bộ khóa.
func (k *AccessKeys) Fetches() int { return int(k.fetches.Load()) }

// Sign ký thẻ bằng khóa đang phục vụ (RS256, header kid).
func (k *AccessKeys) Sign(c AccessClaims) string {
	k.mu.Lock()
	kid, key := k.kid, k.key
	k.mu.Unlock()
	return k.sign(c, kid, key)
}

// SignWithKid ký bằng khóa đang phục vụ nhưng đặt kid khác (kid lạ với bộ kiểm thẻ).
func (k *AccessKeys) SignWithKid(c AccessClaims, kid string) string {
	k.mu.Lock()
	key := k.key
	k.mu.Unlock()
	return k.sign(c, kid, key)
}

// SignForeign ký bằng một khóa không được phục vụ, cùng kid với khóa đang phục vụ (chữ ký sai).
func (k *AccessKeys) SignForeign(c AccessClaims) string {
	k.mu.Lock()
	kid := k.kid
	k.mu.Unlock()
	return k.sign(c, kid, newRSAKey(k.t))
}

func (k *AccessKeys) sign(c AccessClaims, kid string, key *rsa.PrivateKey) string {
	k.t.Helper()
	iss := c.Iss
	if iss == "" {
		iss = "https://" + k.TeamDomain
	}
	exp := c.Exp
	if exp == 0 {
		exp = 5 * time.Minute
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"email": c.Email,
		"aud":   []string{c.AUD},
		"iss":   iss,
		"iat":   now.Add(-time.Minute).Unix(),
		"exp":   now.Add(exp).Unix(),
		"sub":   "sub-" + c.Email,
	}
	if c.MFA != nil {
		claims[AccessMFAClaim] = c.MFA
	}
	maps.Copy(claims, c.Extra)
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		k.t.Fatalf("testkit: ký thẻ Access thử: %v", err)
	}
	return s
}

func newRSAKey(t testing.TB) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("testkit: sinh khóa RSA thử: %v", err)
	}
	return key
}

// jwksJSON trả bộ khóa theo dạng của Cloudflare Access (keys[] kèm public_cert, ở đây bỏ phần chứng chỉ).
func jwksJSON(kid string, pub *rsa.PublicKey) []byte {
	b64 := base64.RawURLEncoding.EncodeToString
	b, _ := json.Marshal(map[string]any{ //nolint:errchkjson // chỉ chuỗi
		"keys": []map[string]string{{
			"kid": kid, "kty": "RSA", "alg": "RS256", "use": "sig",
			"n": b64(pub.N.Bytes()), "e": b64(big.NewInt(int64(pub.E)).Bytes()),
		}},
	})
	return b
}
