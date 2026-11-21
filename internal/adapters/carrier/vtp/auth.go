package vtp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Tên khóa trong Credentials của tài khoản VTP (tài khoản đăng nhập Partner, trang "Token xác thực").
const (
	credUsername = "username"
	credPassword = "password"
)

// tokenEarly là khoảng coi mã truy cập đã hết hạn trước mốc expired của hãng.
const tokenEarly = time.Minute

// session là một mã truy cập; expires zero = hãng không báo hạn (expired = 0).
type session struct {
	token   string
	expires time.Time
}

// tokenCache giữ mã truy cập trong bộ nhớ của tiến trình theo tài khoản (Global Constraints M3: "giữ trong bộ nhớ của
// worker theo tài khoản"); dùng chung giữa các worker.
type tokenCache struct {
	mu sync.Mutex
	m  map[string]session
}

func newTokenCache() *tokenCache { return &tokenCache{m: map[string]session{}} }

func (c *tokenCache) get(key string, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.m[key]
	if !ok || (!s.expires.IsZero() && !now.Before(s.expires)) {
		return "", false
	}
	return s.token, true
}

func (c *tokenCache) put(key string, s session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = s
}

// drop bỏ mã token của key nếu vẫn là mã đang giữ (worker khác có thể đã thay bằng mã mới).
func (c *tokenCache) drop(key, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.m[key]; ok && s.token == token {
		delete(c.m, key)
	}
}

// sessionKey là khóa bộ nhớ của tài khoản: mã tài khoản CRM, đường dẫn gốc, băm của bộ khóa (đổi khóa → đăng nhập lại).
func sessionKey(acc fulfillment.Account, base string) string {
	h := sha256.Sum256([]byte(acc.Creds[credUsername] + "\x00" + acc.Creds[credPassword]))
	return acc.ID.String() + "|" + base + "|" + hex.EncodeToString(h[:])
}

// token trả mã truy cập còn hạn trong bộ nhớ, hoặc đăng nhập.
func (a *Adapter) token(ctx context.Context, acc fulfillment.Account, base string) (string, error) {
	if tok, ok := a.tokens.get(sessionKey(acc, base), a.now()); ok {
		return tok, nil
	}
	return a.login(ctx, acc, base)
}

type loginBody struct {
	Username string `json:"USERNAME"`
	Password string `json:"PASSWORD"`
}

type loginData struct {
	Token   string `json:"token"`
	Expired int64  `json:"expired"`
}

// login gọi POST /v2/user/Login {USERNAME, PASSWORD} và cất data.token (hạn data.expired, mili giây; 0 = không báo).
// Thiếu khóa → auth, không gọi mạng; sai tài khoản (204, HTTP 401/403) → auth vtp_login.
func (a *Adapter) login(ctx context.Context, acc fulfillment.Account, base string) (string, error) {
	user, pass := acc.Creds[credUsername], acc.Creds[credPassword]
	if user == "" || pass == "" {
		return "", authErr(codeNoCreds)
	}
	rep, err := a.send(ctx, kindLogin, base, http.MethodPost, "/v2/user/Login", "", loginBody{Username: user, Password: pass})
	if err != nil {
		return "", err
	}
	data, _, ce := decode(kindLogin, rep)
	if ce != nil {
		if ce.Class == fulfillment.ClassAuth {
			ce.Code = codeLogin
		}
		return "", ce
	}
	var d loginData
	if err := json.Unmarshal(data, &d); err != nil || d.Token == "" {
		return "", notSent(codeBadReply)
	}
	s := session{token: d.Token}
	if d.Expired > 0 {
		s.expires = time.UnixMilli(d.Expired).Add(-tokenEarly)
	}
	a.tokens.put(sessionKey(acc, base), s)
	return d.Token, nil
}

// CheckConnection đăng nhập mới bằng khóa của tài khoản (không dùng mã trong bộ nhớ): được → OK; sai tài khoản →
// Health{OK: false} và lỗi lớp auth; lỗi khác theo lớp của nó.
func (a *Adapter) CheckConnection(ctx context.Context, acc fulfillment.Account) (fulfillment.Health, error) {
	base, err := a.baseURL(acc)
	if err == nil {
		_, err = a.login(ctx, acc, base)
	}
	if err != nil {
		detail := ""
		if ce, ok := errors.AsType[*fulfillment.CarrierError](err); ok {
			detail = ce.Code
		}
		return fulfillment.Health{Detail: detail}, fmt.Errorf("kiểm kết nối Viettel Post: %w", err)
	}
	return fulfillment.Health{OK: true}, nil
}
