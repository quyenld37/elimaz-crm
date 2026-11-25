// Package fakegraph là máy chủ Meta Graph API giả cho test và dev (kế hoạch M4, Task 146): trạng thái trong bộ nhớ
// (Page, token, hội thoại, bài, comment, live, ứng dụng đã đăng ký), các đường Graph mà CRM gọi, kiểm appsecret_proof
// ở mọi yêu cầu có token, điều khiển lỗi (FailNext, HangAfterRead, DropAfterRead, RefuseNextConn, SetUsagePct,
// RevokeToken), luồng comment live SSE và gửi webhook có chữ ký. Chỉ dùng thư viện chuẩn.
//
// Khi nào không dùng: không gọi Meta thật trong test (Global Constraints M4) — mọi test chạm Graph dùng gói này. Không
// dùng làm mẫu payload webhook thật (payload thật ở internal/adapters/facebook/testdata, Task 147). Không chạy ở
// production: tools/fakegraph từ chối khi CRM_ENV=production.
package fakegraph

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	defaultVersion = "v26.0"
	maxBody        = 4 << 20
	// webhookTimeout là hạn của một lần EmitWebhook.
	webhookTimeout = 10 * time.Second
)

// Options cấu hình máy giả. AppSecret dùng để kiểm appsecret_proof, client_secret và ký webhook.
type Options struct {
	AppID, AppSecret string
	// Version là tiền tố phiên bản (mặc định v26.0).
	Version string
	// Hosts là tên máy thêm vào chứng chỉ TLS (ngoài localhost, *.fbcdn.net, *.fbsbx.com, *.facebook.com…).
	Hosts []string
}

// Call là một yêu cầu máy giả đã nhận. Path không có tiền tố phiên bản, không có dấu "/" đầu.
type Call struct {
	Method, Path string
	Versioned    bool
	Query        url.Values
	Body         []byte
	Token        string
	ProofOK      bool
	Header       http.Header
}

// Server là máy Meta giả. Tạo bằng New (test, httptest TLS) hoặc NewHandler (tools/fakegraph).
type Server struct {
	o    Options
	ts   *httptest.Server
	pool *x509.CertPool
	refs *refuser
	hc   *http.Client

	mu      sync.Mutex
	st      state
	faults  map[string][]fault
	calls   []Call
	usage   int
	streams map[string][]*stream
}

// NewHandler tạo máy giả chưa nghe cổng nào (dùng làm http.Handler).
func NewHandler(o Options) *Server {
	if o.Version == "" {
		o.Version = defaultVersion
	}
	return &Server{
		o: o, st: newState(), faults: map[string][]fault{}, streams: map[string][]*stream{},
		hc: &http.Client{Timeout: webhookTimeout},
	}
}

// New chạy máy giả trên httptest TLS với chứng chỉ tự ký có các tên máy của Meta; đóng khi test kết thúc.
func New(t testing.TB, o Options) *Server {
	t.Helper()
	s := NewHandler(o)
	cert, pool, err := selfSigned(append(defaultHosts(), o.Hosts...))
	if err != nil {
		t.Fatalf("fakegraph: tạo chứng chỉ: %v", err)
	}
	s.pool = pool
	ts := httptest.NewUnstartedServer(s)
	ln := &refuser{Listener: ts.Listener}
	ts.Listener, s.refs = ln, ln
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	// RefuseNextConn cố ý làm hỏng bắt tay TLS: bỏ dòng log lỗi bắt tay của http.Server.
	ts.Config.ErrorLog = slog.NewLogLogger(slog.DiscardHandler, slog.LevelError)
	ts.StartTLS()
	s.ts = ts
	t.Cleanup(func() {
		s.closeStreams()
		ts.Close()
	})
	return s
}

// URL là gốc https://127.0.0.1:<cổng> của máy giả (chỉ với New).
func (s *Server) URL() string { return s.ts.URL }

// Port là cổng máy giả đang nghe (chỉ với New).
func (s *Server) Port() string {
	_, p, _ := net.SplitHostPort(s.ts.Listener.Addr().String())
	return p
}

// CertPool là gốc tin cậy chứa chứng chỉ của máy giả (chỉ với New).
func (s *Server) CertPool() *x509.CertPool { return s.pool }

// Client trả một http.Client tin chứng chỉ của máy giả (chỉ với New), không tự giữ kết nối giữa các test.
func (s *Server) Client() *http.Client {
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: s.pool, MinVersion: tls.VersionTLS12},
	}}
}

// Calls trả các yêu cầu đã nhận tới path (không tiền tố phiên bản); path rỗng là mọi yêu cầu.
func (s *Server) Calls(path string) []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	path = trimSlash(path)
	var out []Call
	for _, c := range s.calls {
		if path == "" || c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

// Proof là appsecret_proof đúng của token.
func (s *Server) Proof(token string) string {
	m := hmac.New(sha256.New, []byte(s.o.AppSecret))
	m.Write([]byte(token))
	return hex.EncodeToString(m.Sum(nil))
}

// ServeHTTP phục vụ một yêu cầu Graph.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, versioned := s.splitPath(r.URL.Path)
	if s.serveFile(w, r, path) {
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, maxBody))
	token := tokenOf(r)
	c := Call{
		Method: r.Method, Path: path, Versioned: versioned, Query: r.URL.Query(), Body: body, Token: token,
		Header: r.Header.Clone(),
	}
	c.ProofOK = token != "" && hmac.Equal([]byte(r.URL.Query().Get("appsecret_proof")), []byte(s.Proof(token)))
	s.mu.Lock()
	s.calls = append(s.calls, c)
	f, hasFault := s.popFault(path)
	usage := s.usage
	s.mu.Unlock()
	if usage > 0 {
		w.Header().Set("X-App-Usage", usageJSON(usage))
	}
	if hasFault && s.applyFault(w, r, f) {
		return
	}
	s.route(w, r, &c)
}

// splitPath bỏ "/" đầu và tiền tố phiên bản.
func (s *Server) splitPath(p string) (string, bool) {
	p = trimSlash(p)
	if rest, ok := strings.CutPrefix(p, s.o.Version+"/"); ok {
		return rest, true
	}
	return p, false
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, path string) bool {
	s.mu.Lock()
	f, isFile := s.st.files[path]
	target, isRedirect := s.st.redirects[path]
	s.mu.Unlock()
	switch {
	case isFile:
		w.Header().Set("Content-Type", f.contentType)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(f.body))
		return true
	case isRedirect:
		http.Redirect(w, r, target, http.StatusFound)
		return true
	}
	return false
}

func tokenOf(r *http.Request) string {
	if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return v
	}
	return r.URL.Query().Get("access_token")
}

// route kiểm token rồi chuyển tới đường phục vụ.
func (s *Server) route(w http.ResponseWriter, r *http.Request, c *Call) {
	if c.Path == "oauth/access_token" {
		s.oauth(w, c)
		return
	}
	if videoID, ok := strings.CutSuffix(c.Path, "/live_comments"); ok {
		if s.authorize(w, c) != nil {
			s.serveStream(w, r, videoID)
		}
		return
	}
	ti := s.authorize(w, c)
	if ti == nil {
		return
	}
	s.dispatch(w, c, ti)
}

// authorize trả thông tin token hoặc nil khi đã trả lỗi: thiếu token (104), proof sai (100), token lạ hay bị thu
// hồi (190). Token ứng dụng "<app id>|<app secret>" chỉ dùng cho debug_token.
func (s *Server) authorize(w http.ResponseWriter, c *Call) *tokenInfo {
	switch {
	case c.Token == "":
		writeError(w, GraphErrorSpec{Code: 104, Type: "OAuthException", Message: "An access token is required to request this resource."})
		return nil
	case !c.ProofOK:
		writeError(w, GraphErrorSpec{Code: 100, Type: "GraphMethodException", Message: "Invalid appsecret_proof provided in the API argument"})
		return nil
	case c.Token == s.o.AppID+"|"+s.o.AppSecret:
		return &tokenInfo{}
	}
	s.mu.Lock()
	ti, ok := s.st.tokens[c.Token]
	var cp tokenInfo
	if ok {
		cp = *ti
	}
	s.mu.Unlock()
	if !ok || cp.revoked {
		writeError(w, GraphErrorSpec{Code: 190, Subcode: 460, Type: "OAuthException", Message: "Error validating access token: The session has been invalidated."})
		return nil
	}
	return &cp
}

// oauth đổi mã đăng nhập lấy token người dùng.
func (s *Server) oauth(w http.ResponseWriter, c *Call) {
	q := c.Query
	if q.Get("client_id") != s.o.AppID || q.Get("client_secret") != s.o.AppSecret {
		writeError(w, GraphErrorSpec{Code: 101, Type: "OAuthException", Message: "Error validating application."})
		return
	}
	s.mu.Lock()
	u, ok := s.st.users[q.Get("code")]
	s.mu.Unlock()
	if !ok {
		writeError(w, GraphErrorSpec{Code: 100, Subcode: 36007, Type: "OAuthException", Message: "Invalid verification code format."})
		return
	}
	writeJSON(w, map[string]any{"access_token": u.Token, "token_type": "bearer", "expires_in": 5183944})
}

// GraphErrorSpec là một lỗi máy giả trả (FailNext). HTTPStatus 0 là 400. RawBody khác rỗng thì trả nguyên văn (vd
// trang HTML của 502) thay JSON lỗi. RegainMinutes > 0 thêm header X-Business-Use-Case-Usage có
// estimated_time_to_regain_access.
type GraphErrorSpec struct {
	HTTPStatus, Code, Subcode int
	Type, Message, TraceID    string
	RawBody, ContentType      string
	RegainMinutes             int
}

func writeError(w http.ResponseWriter, e GraphErrorSpec) {
	status := e.HTTPStatus
	if status == 0 {
		status = http.StatusBadRequest
	}
	if e.RegainMinutes > 0 {
		w.Header().Set("X-Business-Use-Case-Usage", businessUsageJSON(100, e.RegainMinutes))
	}
	if e.RawBody != "" {
		ct := e.ContentType
		if ct == "" {
			ct = "text/html; charset=utf-8"
		}
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, e.RawBody)
		return
	}
	if e.TraceID == "" {
		e.TraceID = "AbCdEfFakeTrace"
	}
	inner := map[string]any{"message": e.Message, "type": e.Type, "code": e.Code, "fbtrace_id": e.TraceID}
	if e.Subcode != 0 {
		inner["error_subcode"] = e.Subcode
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": inner})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func trimSlash(p string) string { return strings.Trim(p, "/") }

// refuser đóng ngay kết nối mới khi được yêu cầu (RefuseNextConn): bắt tay TLS hỏng, bên gọi chưa gửi gì.
type refuser struct {
	net.Listener
	mu     sync.Mutex
	refuse int
}

func (l *refuser) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err //nolint:wrapcheck // trả nguyên lỗi của Listener cho http.Server
		}
		l.mu.Lock()
		drop := l.refuse > 0
		if drop {
			l.refuse--
		}
		l.mu.Unlock()
		if !drop {
			return c, nil
		}
		_ = c.Close()
	}
}
