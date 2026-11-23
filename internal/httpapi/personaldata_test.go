package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"elimaz.com.vn/crm/internal/testkit"
)

// Lỗi 500 và panic mang SĐT, địa chỉ, token: log, error_events, câu trả lời và span của yêu cầu không chứa chúng
// (Task 319).
func TestInternalErrorPersonalDataNotLeaked(t *testing.T) {
	// Không t.Parallel (Task 474): CaptureSpans đổi TracerProvider toàn cục.
	exp := testkit.CaptureSpans(t)

	staff := testkit.OpenStaff(t)
	logs := &bytes.Buffer{}
	d := errDeps(staff, logs)
	routes := map[string]http.HandlerFunc{
		"/test/err": func(w http.ResponseWriter, r *http.Request) {
			writeHandlerError(d, w, r, testkit.PersonalDataError())
		},
		"/test/panic": func(http.ResponseWriter, *http.Request) { panic(testkit.PersonalDataError()) },
	}
	for path, fn := range routes {
		rt := Route{Method: "GET", Pattern: "GET " + path, OperationID: "test", Public: true}
		rec := httptest.NewRecorder()
		chain(d, rt, fn).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s: mã %d, muốn 500", path, rec.Code)
		}
		testkit.AssertNoPersonalData(t, path+" câu trả lời", rec.Body.String())
	}
	rows := readErrorEvents(t, staff)
	if len(rows) != 2 {
		t.Fatalf("error_events có %d dòng, muốn 2", len(rows))
	}
	for _, r := range rows {
		testkit.AssertNoPersonalData(t, "error_events.message", r.Message)
	}
	testkit.AssertNoPersonalData(t, "log api", logs.String())
	spans := exp.GetSpans().Snapshots()
	if len(spans) < 2 {
		t.Fatalf("có %d span, muốn ít nhất 2", len(spans))
	}
	testkit.AssertNoPersonalData(t, "span", testkit.SpanText(spans))
}
