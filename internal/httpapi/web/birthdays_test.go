package web_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

const (
	webBirthdaysPath = "/api/v1/web/birthdays"
	webBdStopPath    = "/api/v1/web/birthdays/stop"
	webBdOutboxPath  = "/api/v1/web/birthdays/outbox"
)

// syncBuffer là bộ đệm log an toàn khi nhiều goroutine ghi.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p) //nolint:wrapcheck // test
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// bdWeb là env website có khóa web.birthdays.write và web.coupons.issue, đồng hồ cố định 15/12/2026 10:00 giờ VN, log
// ghi vào bộ đệm.
type bdWeb struct {
	*env

	regKey, issueKey string
	now              time.Time
	logs             *syncBuffer
}

func newBdWeb(t *testing.T) *bdWeb {
	t.Helper()
	b := &bdWeb{env: newEnv(t, nil), logs: &syncBuffer{}}
	b.now = time.Date(2026, 12, 15, 10, 0, 0, 0, time.FixedZone("ICT", 7*3600))
	b.serve(func(d *httpapi.Deps) {
		d.Now = func() time.Time { return b.now }
		d.Logger = slog.New(slog.NewJSONHandler(b.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		d.ServiceKeys = httpapi.NewServiceKeyVerifier(b.staff.App, nil, httpapi.ServiceKeyOptions{Logger: d.Logger})
	})
	b.regKey, b.issueKey = b.serviceKey("web.birthdays.write"), b.serviceKey("web.coupons.issue")
	return b
}

func (b *bdWeb) post(key, idem, path, body string) *httptest.ResponseRecorder {
	b.t.Helper()
	req := httptest.NewRequestWithContext(b.t.Context(), http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return b.send(req, key, "Idempotency-Key", idem)
}

func bdBody(phone string, day, month int, zalo, mail bool, email string) string {
	s := fmt.Sprintf(`{"phone":%q,"fullName":"Khách %s","day":%d,"month":%d,"zalo":%t,"mail":%t`, phone, phone[len(phone)-3:],
		day, month, zalo, mail)
	if email != "" {
		s += fmt.Sprintf(`,"email":%q`, email)
	}
	return s + "}"
}

type bdResult struct {
	Created    bool `json:"created"`
	Day, Month int
}

type outboxBody struct {
	Items []struct {
		CustomerID string `json:"customerId"`
		Code       string `json:"code"`
		Phone      string `json:"phone"`
		Zalo       bool   `json:"zalo"`
		Mail       bool   `json:"mail"`
		Amount     int64  `json:"amount"`
		StopToken  string `json:"stopToken"`
		ValidTo    string `json:"validTo"`
	} `json:"items"`
}

// [WB05] Website gửi đăng ký sinh nhật: lần đầu created true (khách mới có SĐT), lần sau (khóa khác, ngày khác) created
// false giữ ngày cũ; khóa thiếu quyền → 403; 30/2 → 400 invalid_input; SĐT sai → 422 invalid_phone. Lượt phát trong
// CRM rồi outbox: chỉ khách đồng ý kênh, có mã dừng; dừng theo mã → rời outbox, gọi lại giữ lúc đầu, mã lạ → 404;
// khóa thiếu web.coupons.issue đọc outbox → 403. Audit của khóa dịch vụ không chứa SĐT; log không chứa SĐT, mã dừng.
func TestWebBirthday_RegisterOutboxStop(t *testing.T) {
	t.Parallel()
	b := newBdWeb(t)
	r := decodeOK[bdResult](t, "đăng ký", b.post(b.regKey, "r1", webBirthdaysPath,
		bdBody("0912000001", 18, 12, true, false, "")))
	if !r.Created || r.Day != 18 || r.Month != 12 {
		t.Fatalf("đăng ký: %+v", r)
	}
	again := decodeOK[bdResult](t, "đăng ký lại", b.post(b.regKey, "r2", webBirthdaysPath,
		bdBody("+84 912 000 001", 2, 3, false, false, "")))
	if again.Created || again.Day != 18 || again.Month != 12 {
		t.Fatalf("đăng ký lại: %+v", again)
	}
	decodeOK[bdResult](t, "khách không đồng ý kênh", b.post(b.regKey, "r3", webBirthdaysPath,
		bdBody("0912000002", 18, 12, false, false, "")))
	decodeOK[bdResult](t, "khách nhận email", b.post(b.regKey, "r4", webBirthdaysPath,
		bdBody("0912000003", 18, 12, false, true, "khach3@example.com")))
	b.checkRegisterRefusals()
	if n := b.staff.CountIn(t, b.tenant, `SELECT count(*) FROM customer.birthday_profiles`); n != 3 {
		t.Fatalf("số hồ sơ: %d", n)
	}

	// Đủ điều kiện nhận quà: có đơn giao, đăng ký từ 60 ngày trước; nhân viên bật quà và chạy lượt phát.
	b.staff.Exec(t, b.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE customer.customers SET delivered_count = 1`)
		if err == nil {
			_, err = tx.Exec(t.Context(), `UPDATE customer.birthday_profiles SET created_at = created_at - interval '60 days'`)
		}
		return err //nolint:wrapcheck // test
	})
	b.app(func(tx pgx.Tx) error {
		s := promotion.DefaultBirthdaySettings()
		s.Enabled = true
		if _, _, err := (promotion.Writer{}).SaveBirthdaySettings(t.Context(), tx, s, 1, promotion.Actor{UserID: b.owner.UserID}, b.now); err != nil {
			return err //nolint:wrapcheck // test
		}
		run, err := promotion.Writer{}.RunBirthdayGifts(t.Context(), tx, promotion.BirthdayTriggerManual,
			promotion.Actor{UserID: b.owner.UserID}, b.now)
		if err == nil && run.Given != 3 {
			t.Errorf("lượt phát: %+v", run)
		}
		return err //nolint:wrapcheck // test
	})

	out := decodeOK[outboxBody](t, "outbox", b.get(b.issueKey, webBdOutboxPath))
	if len(out.Items) != 2 {
		t.Fatalf("outbox: %+v", out)
	}
	zalo := zaloStopToken(t, out)
	if rec := b.get(b.regKey, webBdOutboxPath); rec.Code != http.StatusForbidden {
		t.Errorf("outbox bằng khóa thiếu quyền: %d", rec.Code)
	}

	stop := func(idem, token string) *httptest.ResponseRecorder {
		return b.post(b.regKey, idem, webBdStopPath, fmt.Sprintf(`{"token":%q}`, token))
	}
	first := decodeOK[map[string]string](t, "dừng", stop("s1", zalo))
	b.now = b.now.Add(time.Hour)
	second := decodeOK[map[string]string](t, "dừng lại", stop("s2", zalo))
	if first["stoppedAt"] == "" || first["stoppedAt"] != second["stoppedAt"] {
		t.Errorf("lúc dừng: %v %v", first, second)
	}
	if rec := stop("s3", strings.Repeat("z", 43)); rec.Code != http.StatusNotFound {
		t.Errorf("mã dừng lạ: %d %s", rec.Code, rec.Body.String())
	}
	if out := decodeOK[outboxBody](t, "outbox sau khi dừng", b.get(b.issueKey, webBdOutboxPath)); len(out.Items) != 1 ||
		out.Items[0].Phone != "+84912000003" {
		t.Errorf("outbox sau khi dừng: %+v", out)
	}
	b.checkNoPII(zalo)
}

// checkRegisterRefusals: khóa thiếu quyền → 403; 30/2, nhận email không có email → 400; SĐT sai → 422.
func (b *bdWeb) checkRegisterRefusals() {
	t := b.t
	t.Helper()
	n := 0
	for name, c := range map[string]struct {
		key, body string
		status    int
		code      string
	}{
		"thiếu quyền": {b.key, bdBody("0912000009", 1, 1, false, false, ""), http.StatusForbidden, "forbidden"},
		"30/2":        {b.regKey, bdBody("0912000009", 30, 2, false, false, ""), http.StatusBadRequest, "invalid_input"},
		"mail không email": {
			b.regKey, bdBody("0912000009", 1, 1, false, true, ""), http.StatusBadRequest,
			"invalid_input",
		},
		"SĐT sai": {
			b.regKey, bdBody("0012345678", 1, 1, false, false, ""), http.StatusUnprocessableEntity,
			"invalid_phone",
		},
	} {
		n++
		rec := b.post(c.key, fmt.Sprintf("bad-%d", n), webBirthdaysPath, c.body)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), `"error":"`+c.code+`"`) {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
}

// zaloStopToken kiểm từng mục outbox (có mã, mã dừng, mệnh giá, hạn; đúng kênh) và trả mã dừng của khách Zalo.
func zaloStopToken(t *testing.T, out outboxBody) string {
	t.Helper()
	var zalo string
	for _, it := range out.Items {
		if it.Code == "" || it.StopToken == "" || it.Amount != 50000 || it.ValidTo == "" {
			t.Errorf("mục outbox: %+v", it)
		}
		switch it.Phone {
		case "+84912000001":
			zalo = it.StopToken
			if !it.Zalo || it.Mail {
				t.Errorf("kênh Zalo: %+v", it)
			}
		case "+84912000003":
			if it.Zalo || !it.Mail {
				t.Errorf("kênh email: %+v", it)
			}
		default:
			t.Errorf("khách không đồng ý kênh nào vào outbox: %+v", it)
		}
	}
	return zalo
}

// checkNoPII: audit của khóa dịch vụ đủ dòng và không chứa SĐT; log không chứa SĐT, email, mã dừng.
func (b *bdWeb) checkNoPII(zalo string) {
	t := b.t
	t.Helper()
	if n := b.staff.CountIn(t, b.tenant, `SELECT count(*) FROM identity.audit_logs WHERE actor_type = 'service'
		AND action IN ('customer.birthday.register', 'customer.birthday.stop')`); n != 4 {
		t.Errorf("audit của khóa dịch vụ: %d, muốn 3 đăng ký + 1 dừng", n)
	}
	if n := b.staff.CountIn(t, b.tenant, `SELECT count(*) FROM identity.audit_logs
		WHERE row_to_json(audit_logs)::text LIKE '%912000%'`); n != 0 {
		t.Errorf("audit chứa SĐT: %d", n)
	}
	logs := b.logs.String()
	for _, bad := range []string{"912000001", "912000003", "khach3@", zalo} {
		if strings.Contains(logs, bad) {
			t.Errorf("log chứa %q", bad)
		}
	}
}
