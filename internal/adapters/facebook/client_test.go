package facebook_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

const (
	testAppID     = "1626301872631379"
	testAppSecret = "0123456789abcdef0123456789abcdef"
	testPageID    = "1000111"
	testToken     = "EAAB-test-1"
	testPSID      = "7001"
)

type env struct {
	fg  *fakegraph.Server
	c   *facebook.Client
	log *bytes.Buffer
	reg *metrics.Registry
}

// newEnv dựng máy Meta giả có một Page và client trỏ vào nó; appSecret rỗng là dùng đúng secret của máy giả.
func newEnv(t *testing.T, appSecret string) env {
	t.Helper()
	fg := fakegraph.New(t, fakegraph.Options{AppID: testAppID, AppSecret: testAppSecret})
	fg.AddPage(fakegraph.Page{ID: testPageID, Name: "Shop thử", Token: testToken})
	fg.AddProfile(fakegraph.Profile{PSID: testPSID, FirstName: "Lan", LastName: "Nguyễn"})
	if appSecret == "" {
		appSecret = testAppSecret
	}
	var buf bytes.Buffer
	reg := metrics.NewRegistry()
	c := facebook.NewClient(facebook.Options{
		BaseURL: fg.URL(), StreamingBaseURL: fg.URL(), Version: "v26.0", AppID: testAppID,
		AppSecret: logging.Secret(appSecret), HTTP: fg.Client(), Metrics: reg,
		Logger:          slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
		AttachmentHosts: []string{"fbcdn.net", "fbsbx.com"},
	})
	return env{fg: fg, c: c, log: &buf, reg: reg}
}

func sendBody(text string) map[string]any {
	return map[string]any{"recipient": map[string]any{"id": testPSID}, "message": map[string]any{"text": text}, "messaging_type": "RESPONSE"}
}

type sendReply struct {
	RecipientID string `json:"recipient_id"`
	MessageID   string `json:"message_id"`
}

func TestDo_VersionPrefixAndProof(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "")
	ctx := t.Context()
	var out sendReply
	if _, err := e.c.Do(ctx, http.MethodPost, testPageID+"/messages", testToken, nil, sendBody("chào"), &out); err != nil {
		t.Fatalf("gửi: %v", err)
	}
	if out.MessageID == "" || out.RecipientID != testPSID {
		t.Fatalf("trả lời gửi = %+v", out)
	}
	var prof map[string]any
	if _, err := e.c.Do(ctx, http.MethodGet, testPSID, testToken, url.Values{"fields": {"first_name"}}, nil, &prof); err != nil {
		t.Fatalf("đọc hồ sơ: %v", err)
	}
	calls := e.fg.Calls("")
	if len(calls) != 2 {
		t.Fatalf("mong 2 lời gọi, có %d", len(calls))
	}
	for _, c := range calls {
		if !c.Versioned || !c.ProofOK {
			t.Errorf("%s %s: có tiền tố v26.0 = %v, appsecret_proof đúng = %v", c.Method, c.Path, c.Versioned, c.ProofOK)
		}
		if c.Query.Get("access_token") != "" {
			t.Errorf("%s: token nằm trong URL", c.Path)
		}
	}
}

func TestDo_WrongAppSecretRejected(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "ffffffffffffffffffffffffffffffff")
	_, err := e.c.Do(t.Context(), http.MethodGet, testPSID, testToken, nil, nil, nil)
	ge, ok := errors.AsType[*facebook.GraphError](err)
	if !ok || ge.Code != 100 {
		t.Fatalf("mong GraphError mã 100, có %v", err)
	}
	if got := facebook.Classify(err); got != channel.ErrorPermanent {
		t.Fatalf("lớp = %s, mong permanent", got)
	}
}

func TestDo_NoSecretsInLogsAndMessages(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "")
	ctx := t.Context()
	_, _ = e.c.Do(ctx, http.MethodGet, testPSID, testToken, nil, nil, nil)
	e.fg.FailNext(testPageID+"/messages", fakegraph.GraphErrorSpec{
		Code: 100, Type: "OAuthException", Message: "Số 0912345678 không hợp lệ, token " + testToken,
	})
	_, err := e.c.Do(ctx, http.MethodPost, testPageID+"/messages", testToken, nil, sendBody("số 0912345678"), nil)
	ge, ok := errors.AsType[*facebook.GraphError](err)
	if !ok {
		t.Fatalf("mong GraphError, có %v", err)
	}
	e.fg.RefuseNextConn()
	_, errNet := e.c.Do(ctx, http.MethodGet, testPSID, testToken, nil, nil, nil)
	for name, s := range map[string]string{"log": e.log.String(), "message": ge.Message, "error": err.Error(), "lỗi mạng": errNet.Error()} {
		for _, secret := range []string{testToken, "0912345678", testAppSecret} {
			if strings.Contains(s, secret) {
				t.Errorf("%s chứa %q: %s", name, secret, s)
			}
		}
	}
	if !strings.Contains(e.log.String(), `"level":"DEBUG"`) {
		t.Fatalf("mong có log debug cho mỗi lời gọi, log: %s", e.log.String())
	}
}

func TestDo_MetricsLabelWithoutIDs(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "")
	_, _ = e.c.Do(t.Context(), http.MethodPost, testPageID+"/messages", testToken, nil, sendBody("a"), nil)
	e.fg.FailNext(testPSID, fakegraph.GraphErrorSpec{Code: 190, Type: "OAuthException", Message: "x"})
	_, _ = e.c.Do(t.Context(), http.MethodGet, testPSID, testToken, nil, nil, nil)
	labels := map[string]bool{}
	for _, r := range e.reg.Drain() {
		labels[r.Name+" "+r.Label] = true
		if strings.Contains(r.Label, testPageID) || strings.Contains(r.Label, testPSID) {
			t.Errorf("nhãn chỉ số chứa mã đối tượng: %s %s", r.Name, r.Label)
		}
	}
	for _, want := range []string{"graph_ms POST :id/messages", "graph_ms GET :id", "graph_errors 190"} {
		if !labels[want] {
			t.Errorf("thiếu chỉ số %q trong %v", want, labels)
		}
	}
}

// TestDo_SentState: chưa gửi trọn → not_sent; máy giả đã đọc thân rồi đứt, treo, 502 HTML → maybe_sent, unknown.
func TestDo_SentState(t *testing.T) {
	t.Parallel()
	path := testPageID + "/messages"
	cases := []struct {
		name   string
		arm    func(fg *fakegraph.Server)
		sent   facebook.SentState
		class  channel.ErrorClass
		status int
	}{
		{"đóng kết nối trước khi đọc", func(fg *fakegraph.Server) { fg.RefuseNextConn() }, facebook.NotSent, channel.ErrorNotSentRetry, 0},
		{"đọc thân rồi đóng", func(fg *fakegraph.Server) { fg.DropAfterRead(path) }, facebook.MaybeSent, channel.ErrorUnknown, 0},
		{"502 thân HTML", func(fg *fakegraph.Server) {
			fg.FailNext(path, fakegraph.GraphErrorSpec{HTTPStatus: http.StatusBadGateway, RawBody: "<html>Bad Gateway</html>"})
		}, facebook.MaybeSent, channel.ErrorUnknown, http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "")
			tc.arm(e.fg)
			_, err := e.c.Do(t.Context(), http.MethodPost, path, testToken, nil, sendBody("chào"), nil)
			ge, ok := errors.AsType[*facebook.GraphError](err)
			if !ok {
				t.Fatalf("mong GraphError, có %v", err)
			}
			if ge.Sent != tc.sent || facebook.Classify(err) != tc.class || ge.HTTPStatus != tc.status {
				t.Fatalf("Sent=%s lớp=%s HTTP=%d; mong %s %s %d", ge.Sent, facebook.Classify(err), ge.HTTPStatus, tc.sent, tc.class, tc.status)
			}
		})
	}
}

// TestDo_HangAfterReadTimesOut: máy giả nhận rồi treo 11 s → client bỏ sau đúng hạn 10 s, kết quả không rõ.
func TestDo_HangAfterReadTimesOut(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "")
	path := testPageID + "/messages"
	e.fg.HangAfterRead(path, 11*time.Second)
	start := time.Now()
	_, err := e.c.Do(t.Context(), http.MethodPost, path, testToken, nil, sendBody("chào"), nil)
	took := time.Since(start)
	ge, ok := errors.AsType[*facebook.GraphError](err)
	if !ok || ge.Sent != facebook.MaybeSent || facebook.Classify(err) != channel.ErrorUnknown {
		t.Fatalf("mong maybe_sent/unknown, có %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("mong lỗi hết hạn, có %v", err)
	}
	// Hạn 10 s là của client (máy giả treo 11 s), nên không thể bỏ sớm hơn; trần 20 s chỉ là chốt chống treo
	// (không lấy giờ thật làm tiêu chí đúng/sai, CLAUDE.md "Bẫy đã gặp", Task 400).
	if took < 9500*time.Millisecond || took > 20*time.Second {
		t.Fatalf("bỏ sau %v, mong theo hạn 10 s", took)
	}
	if len(e.fg.Calls(path)) != 1 {
		t.Fatal("client không được tự gửi lại")
	}
}

func TestDo_RateLimitRetryAfterFromUsage(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "")
	path := testPageID + "/messages"
	e.fg.FailNext(path, fakegraph.GraphErrorSpec{Code: 613, Type: "OAuthException", Message: "Calls exceeded", RegainMinutes: 5})
	meta, err := e.c.Do(t.Context(), http.MethodPost, path, testToken, nil, sendBody("a"), nil)
	if facebook.Classify(err) != channel.ErrorRateLimited {
		t.Fatalf("lớp = %s, mong rate_limited (%v)", facebook.Classify(err), err)
	}
	if got := facebook.RetryAfter(err); got != 5*time.Minute {
		t.Fatalf("RetryAfter = %v, mong 5 phút", got)
	}
	if meta.Usage.RegainAfter != 5*time.Minute || !meta.Usage.ShouldSlowDown() {
		t.Fatalf("Meta.Usage = %+v", meta.Usage)
	}
}

func TestDo_UsageHeader(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "")
	for _, tc := range []struct {
		pct  int
		slow bool
	}{{85, true}, {79, false}, {80, true}} {
		e.fg.SetUsagePct(tc.pct)
		meta, err := e.c.Do(t.Context(), http.MethodGet, testPSID, testToken, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if meta.Usage.CallCountPct != tc.pct || meta.Usage.ShouldSlowDown() != tc.slow {
			t.Errorf("pct %d: Usage = %+v, ShouldSlowDown = %v", tc.pct, meta.Usage, meta.Usage.ShouldSlowDown())
		}
	}
}

func TestParseUsage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		header map[string]string
		want   facebook.Usage
		slow   bool
	}{
		{"app 85", map[string]string{"X-App-Usage": `{"call_count":85}`}, facebook.Usage{CallCountPct: 85}, true},
		{"app 79", map[string]string{"X-App-Usage": `{"call_count":79}`}, facebook.Usage{CallCountPct: 79}, false},
		{"business lấy lớn nhất", map[string]string{
			"X-App-Usage":               `{"call_count":10,"total_time":5,"total_cputime":3}`,
			"X-Business-Use-Case-Usage": `{"123":[{"type":"pages","call_count":20,"total_time":90,"total_cputime":4,"estimated_time_to_regain_access":0},{"type":"messenger","call_count":30,"total_time":1,"total_cputime":2,"estimated_time_to_regain_access":7}]}`,
		}, facebook.Usage{CallCountPct: 30, TotalTimePct: 90, TotalCPUPct: 4, RegainAfter: 7 * time.Minute}, true},
		{"không header", nil, facebook.Usage{}, false},
		{"JSON hỏng bỏ qua", map[string]string{"X-App-Usage": `{call_count`}, facebook.Usage{}, false},
	}
	for _, tc := range cases {
		h := http.Header{}
		for k, v := range tc.header {
			h.Set(k, v)
		}
		got := facebook.ParseUsage(h)
		if got != tc.want || got.ShouldSlowDown() != tc.slow {
			t.Errorf("%s: %+v (chậm %v), mong %+v (chậm %v)", tc.name, got, got.ShouldSlowDown(), tc.want, tc.slow)
		}
	}
}
