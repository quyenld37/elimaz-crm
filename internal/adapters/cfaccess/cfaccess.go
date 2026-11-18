// Package cfaccess là bộ nối tới Cloudflare Access: kiểm thẻ JWT mà Access gắn vào header cf-access-jwt-assertion
// (ADR-10, ADR-35). Gói tải khóa công khai từ https://<TeamDomain>/cdn-cgi/access/certs, giữ trong bộ nhớ, kiểm chữ
// ký RS256, aud, iss, exp và dấu đã qua bước hai (luật identity.CheckMFA). Hiện thực identity.Verifier; cmd/crm nối
// vào lớp HTTP. Module nghiệp vụ không nhập gói này (không gọi mạng).
package cfaccess

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
)

// Nhịp tải khóa (kế hoạch M0, Task 7).
const (
	refreshEvery   = 10 * time.Minute // làm mới định kỳ
	minFetchGap    = 30 * time.Second // tối đa một lần tải mỗi 30 s (kid lạ, hoặc lần trước lỗi)
	fetchTimeout   = 5 * time.Second
	maxCertsBytes  = 1 << 20
	minRSAKeyBits  = 2048
	maxTokenLength = 16 << 10 // thẻ Access thật dưới 2 KiB; chặn thẻ khổng lồ trước khi giải mã
)

// ErrOptions báo Options thiếu hay sai (api không khởi động).
var ErrOptions = errors.New("cfaccess: cấu hình Cloudflare Access không hợp lệ")

// Options là cấu hình của bộ kiểm thẻ (ACCESS_TEAM_DOMAIN, ACCESS_AUD, ACCESS_MFA_CLAIM, ACCESS_MFA_VALUES).
type Options struct {
	TeamDomain string   // ví dụ elimaz.cloudflareaccess.com (không có https://)
	AUD        string   // Application Audience tag của ứng dụng Access
	MFAClaim   string   // tên trường mang dấu bước hai
	MFAValues  []string // giá trị chấp nhận của trường đó (ít nhất một)
	HTTPClient *http.Client
	Now        func() time.Time
}

// Verifier kiểm thẻ Cloudflare Access; an toàn khi dùng từ nhiều goroutine.
type Verifier struct {
	o      Options
	certs  string
	parser *jwt.Parser

	mu        sync.Mutex // giữ trong lúc tải: yêu cầu khác chờ rồi thấy khóa mới, không tải lần hai
	keys      map[string]*rsa.PublicKey
	loadedAt  time.Time // lần tải thành công gần nhất
	attempted time.Time // lần thử tải gần nhất (thành công hay lỗi)
}

// New kiểm Options và trả bộ kiểm thẻ. Khóa được tải ở lần kiểm thẻ đầu tiên (khởi động không gọi mạng).
func New(o Options) (*Verifier, error) {
	var problems []string
	if o.TeamDomain == "" || strings.ContainsAny(o.TeamDomain, "/?#@ ") {
		problems = append(problems, "TeamDomain phải là tên miền (không có https:// hay đường dẫn)")
	}
	if o.AUD == "" {
		problems = append(problems, "thiếu AUD")
	}
	if o.MFAClaim == "" {
		problems = append(problems, "thiếu MFAClaim")
	}
	if len(o.MFAValues) == 0 {
		problems = append(problems, "MFAValues rỗng (mọi thẻ sẽ bị từ chối)")
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrOptions, strings.Join(problems, "; "))
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	issuer := "https://" + o.TeamDomain
	return &Verifier{
		o:     o,
		certs: issuer + "/cdn-cgi/access/certs",
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
			jwt.WithAudience(o.AUD),
			jwt.WithIssuer(issuer),
			jwt.WithExpirationRequired(),
			jwt.WithTimeFunc(o.Now),
		),
	}, nil
}

// Verify kiểm thẻ và trả email trong thẻ. Lỗi bọc identity.ErrInvalidToken hoặc identity.ErrMFARequired; lỗi không
// mang nội dung thẻ.
func (v *Verifier) Verify(ctx context.Context, token string) (string, error) {
	if token == "" || len(token) > maxTokenLength {
		return "", identity.ErrInvalidToken
	}
	claims := jwt.MapClaims{}
	_, err := v.parser.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if key := v.key(ctx, kid); key != nil {
			return key, nil
		}
		return nil, errUnknownKid
	})
	if err != nil {
		return "", fmt.Errorf("%w: %w", identity.ErrInvalidToken, reason(err))
	}
	email, _ := claims["email"].(string)
	if strings.TrimSpace(email) == "" {
		return "", fmt.Errorf("%w: thẻ thiếu email", identity.ErrInvalidToken)
	}
	if err := identity.CheckMFA(claims[v.o.MFAClaim], v.o.MFAValues); err != nil {
		return "", err //nolint:wrapcheck // sentinel của identity, lớp HTTP so bằng errors.Is
	}
	return email, nil
}

var (
	errUnknownKid = errors.New("không có khóa cho kid của thẻ")
	errFetch      = errors.New("cfaccess: bộ khóa Access không dùng được")
)

// reason giữ lý do ngắn của thư viện jwt (lỗi sentinel, không chứa nội dung thẻ).
func reason(err error) error {
	for _, e := range []error{
		jwt.ErrTokenExpired, jwt.ErrTokenInvalidAudience, jwt.ErrTokenInvalidIssuer,
		jwt.ErrTokenSignatureInvalid, jwt.ErrTokenMalformed, jwt.ErrTokenRequiredClaimMissing, jwt.ErrTokenNotValidYet,
		jwt.ErrTokenUsedBeforeIssued, jwt.ErrTokenUnverifiable, errUnknownKid,
	} {
		if errors.Is(err, e) {
			return e
		}
	}
	return jwt.ErrTokenInvalidClaims
}

// key trả khóa của kid, tải lại khi khóa cũ hơn 10 phút hoặc kid lạ — tối đa một lần mỗi 30 s. Tải lỗi thì giữ khóa
// cũ. nil khi không có khóa cho kid.
func (v *Verifier) key(ctx context.Context, kid string) *rsa.PublicKey {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.o.Now()
	k, ok := v.keys[kid]
	stale := v.loadedAt.IsZero() || now.Sub(v.loadedAt) >= refreshEvery
	if (ok && !stale) || (!v.attempted.IsZero() && now.Sub(v.attempted) < minFetchGap) {
		return k
	}
	v.attempted = now
	if keys, err := v.fetch(ctx); err == nil {
		v.keys, v.loadedAt = keys, now
		k = keys[kid]
	}
	return k
}

type jwks struct {
	Keys []struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		Alg string `json:"alg"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

// fetch tải bộ khóa; chỉ giữ khóa RSA dùng cho RS256, tối thiểu 2048 bit. Bộ rỗng là lỗi (không xóa khóa cũ).
func (v *Verifier) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	// Không gắn hạn của yêu cầu đang chờ vào lần tải: yêu cầu bị hủy không được làm hỏng lần tải chung.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certs, nil)
	if err != nil {
		return nil, fmt.Errorf("tạo yêu cầu tải khóa: %w", err)
	}
	resp, err := v.o.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tải khóa Access: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: tải khóa trả %d", errFetch, resp.StatusCode)
	}
	var set jwks
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxCertsBytes)).Decode(&set); err != nil {
		return nil, fmt.Errorf("đọc bộ khóa Access: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kid == "" || k.Kty != "RSA" || (k.Alg != "" && k.Alg != jwt.SigningMethodRS256.Alg()) {
			continue
		}
		if pub, ok := rsaKey(k.N, k.E); ok {
			keys[k.Kid] = pub
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: không có khóa RSA dùng được", errFetch)
	}
	return keys, nil
}

func rsaKey(n64, e64 string) (*rsa.PublicKey, bool) {
	nb, err1 := base64.RawURLEncoding.DecodeString(n64)
	eb, err2 := base64.RawURLEncoding.DecodeString(e64)
	if err1 != nil || err2 != nil || len(eb) == 0 || len(eb) > 4 {
		return nil, false
	}
	n := new(big.Int).SetBytes(nb)
	e := int(new(big.Int).SetBytes(eb).Int64())
	if n.BitLen() < minRSAKeyBits || e < 3 || e%2 == 0 {
		return nil, false
	}
	return &rsa.PublicKey{N: n, E: e}, true
}
