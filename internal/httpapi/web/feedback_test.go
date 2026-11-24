package web_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

const webFeedbackPath = "/api/v1/web/feedback"

// fbWeb là env website có khóa web.feedback.write (key), khóa thiếu quyền (wrong), log ghi vào bộ đệm, và khách "Chị
// Thu" SĐT 0912777888 (known).
type fbWeb struct {
	*env

	key, wrong string
	logs       *syncBuffer
	known      uuid.UUID
}

func newFbWeb(t *testing.T) *fbWeb {
	t.Helper()
	f := &fbWeb{env: newEnv(t, nil), logs: &syncBuffer{}}
	f.serve(func(d *httpapi.Deps) {
		d.Logger = slog.New(slog.NewJSONHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		d.ServiceKeys = httpapi.NewServiceKeyVerifier(f.staff.App, nil, httpapi.ServiceKeyOptions{Logger: d.Logger})
	})
	f.key, f.wrong = f.serviceKey("web.feedback.write"), f.serviceKey("web.birthdays.write")
	if err := tenancy.WithTenant(t.Context(), f.staff.App, f.tenant, func(tx pgx.Tx) error {
		c, err := customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}.CreateCustomer(t.Context(), tx,
			customer.CreateInput{FullName: "Chị Thu", Phone: "0912777888"}, customer.Actor{Type: customer.ActorSystem},
			time.Now().UTC())
		f.known = c.ID
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	return f
}

// post gửi body bằng khóa key với Idempotency-Key idem (rỗng: không gửi header).
func (f *fbWeb) post(key, idem, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(f.t.Context(), http.MethodPost, webFeedbackPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if idem == "" {
		return f.send(req, key)
	}
	return f.send(req, key, "Idempotency-Key", idem)
}

// created đòi 201 và trả mã góp ý.
func (f *fbWeb) created(name string, rec *httptest.ResponseRecorder) uuid.UUID {
	f.t.Helper()
	var out struct {
		ID uuid.UUID `json:"id"`
	}
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		f.t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
	}
	return out.ID
}

func (f *fbWeb) count(q string, args ...any) int {
	f.t.Helper()
	return f.staff.CountIn(f.t, f.tenant, q, args...)
}

// [Task 455] Website gửi góp ý: khóa thiếu web.feedback.write → 403; thiếu Idempotency-Key → 400; góp ý có SĐT của khách
// có sẵn → 201, gắn khách; gửi lại cùng khóa → cùng mã, một dòng; SĐT lạ → không gắn, không tạo khách. Log và audit
// không chứa nội dung, SĐT, tên.
func TestWebFeedback_Submit(t *testing.T) {
	t.Parallel()
	f := newFbWeb(t)
	body := `{"topic":"delivery","mood":4,"body":"Giao nhanh noi-dung-rieng","name":"Thu Trần","contact":"0912 777 888",` +
		`"orderRef":"W1001","page":"/gop-y","device":"iPhone","clientHash":"abc123"}`
	if rec := f.post(f.wrong, "f0", body); rec.Code != http.StatusForbidden {
		t.Errorf("khóa thiếu quyền: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.post(f.key, "", body); rec.Code != http.StatusBadRequest {
		t.Errorf("thiếu Idempotency-Key: %d %s", rec.Code, rec.Body.String())
	}
	id := f.created("gửi góp ý", f.post(f.key, "f1", body))
	if again := f.created("gửi lại cùng khóa", f.post(f.key, "f1", body)); again != id {
		t.Errorf("gửi lại cùng khóa: %s, mong %s", again, id)
	}
	if n := f.count(`SELECT count(*) FROM customer.feedback WHERE id = $1 AND customer_id = $2 AND topic = 'delivery'
		AND mood = 4 AND client_hash = 'abc123' AND state = 'new'`, id, f.known); n != 1 {
		t.Errorf("góp ý gắn khách: %d", n)
	}
	customers := f.count(`SELECT count(*) FROM customer.customers`)
	stranger := f.created("SĐT lạ", f.post(f.key, "f2", `{"topic":"other","body":"Khách lạ","contact":"0909123456"}`))
	if n := f.count(`SELECT count(*) FROM customer.feedback WHERE id = $1 AND customer_id IS NULL`, stranger); n != 1 {
		t.Errorf("SĐT lạ không gắn: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM customer.customers`); n != customers {
		t.Errorf("SĐT lạ tạo khách: %d → %d", customers, n)
	}
	if n := f.count(`SELECT count(*) FROM customer.feedback`); n != 2 {
		t.Errorf("số góp ý: %d", n)
	}
	var audit string
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT coalesce(string_agg(row_to_json(a)::text, ' '), '')
			FROM identity.audit_logs a WHERE action LIKE 'customer.feedback.%'`).Scan(&audit)
	})
	if !strings.Contains(audit, "customer.feedback.submit") {
		t.Errorf("thiếu audit customer.feedback.submit: %s", audit)
	}
	for _, bad := range []string{"noi-dung-rieng", "912777888", "909123456", "Thu Trần", "Khách lạ"} {
		if strings.Contains(audit, bad) || strings.Contains(f.logs.String(), bad) {
			t.Errorf("audit hay log chứa %q", bad)
		}
	}
}

// [Task 455] Trường sai: nội dung 2001 ký tự, chủ đề lạ, điểm 6 → 400 bad_request (ogen kiểm theo hợp đồng trước
// handler); nội dung chỉ khoảng trắng → 400 invalid_input (module kiểm); không lưu gì. Đúng 2000 ký tự → 201.
func TestWebFeedback_Invalid(t *testing.T) {
	t.Parallel()
	f := newFbWeb(t)
	for i, c := range []struct{ body, code string }{
		{fmt.Sprintf(`{"topic":"product","body":%q}`, strings.Repeat("ơ", 2001)), "bad_request"},
		{`{"topic":"price","body":"x"}`, "bad_request"},
		{`{"topic":"product","body":"x","mood":6}`, "bad_request"},
		{`{"topic":"product","body":"   "}`, "invalid_input"},
	} {
		rec := f.post(f.key, fmt.Sprintf("bad%d", i), c.body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"`+c.code+`"`) {
			t.Errorf("ca sai %d: %d %s, mong 400 %s", i, rec.Code, rec.Body.String(), c.code)
		}
	}
	if n := f.count(`SELECT count(*) FROM customer.feedback`); n != 0 {
		t.Errorf("ca sai vẫn lưu: %d", n)
	}
	f.created("đúng 2000 ký tự", f.post(f.key, "ok", fmt.Sprintf(`{"topic":"product","body":%q}`, strings.Repeat("ơ", 2000))))
}
