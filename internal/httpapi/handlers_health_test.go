package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testHealthToken = "thu-health-token-du-dai-32-ky-tu-abcdef"

func deepGet(t *testing.T, srv http.Handler, auth string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health/deep", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func beat(t *testing.T, d Deps, process, role string, age time.Duration) {
	t.Helper()
	if _, err := d.DB.Pool.Exec(t.Context(), `INSERT INTO platform.process_heartbeats (process, role, at)
		VALUES ($1, $2, now() - make_interval(secs => $3)) ON CONFLICT (process) DO UPDATE SET at = EXCLUDED.at`,
		process, role, age.Seconds()); err != nil {
		t.Fatal(err)
	}
}

// /health/deep: thiếu hay sai thẻ → 401; worker 200 s tuổi → 503 ok:false; mọi thứ tốt → 200 ok:true.
func TestHealthDeep(t *testing.T) {
	var buf bytes.Buffer
	d := testDeps(t, &buf)
	d.Config.HealthToken = testHealthToken
	srv := NewServer(d)

	for _, auth := range []string{"", "Bearer sai", "Basic " + testHealthToken, testHealthToken} {
		rec := deepGet(t, srv, auth)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("thẻ %q: mong 401, có %d", auth, rec.Code)
		}
		if b := decodeErr(t, rec); b.Error != codeUnauthenticated {
			t.Fatalf("mã lỗi %q", b.Error)
		}
	}

	beat(t, d, "worker@a", "worker", 200*time.Second)
	beat(t, d, "ingest@a", "ingest", 5*time.Second)
	rec := deepGet(t, srv, "Bearer "+testHealthToken)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || body["ok"] != false {
		t.Fatalf("worker 200 s: mong 503 ok:false, có %d %s", rec.Code, rec.Body)
	}
	if age, _ := body["workerHeartbeatAgeS"].(float64); age < 199 || age > 230 {
		t.Fatalf("workerHeartbeatAgeS = %v", body["workerHeartbeatAgeS"])
	}

	beat(t, d, "worker@a", "worker", 5*time.Second)
	rec = deepGet(t, srv, "Bearer "+testHealthToken)
	body = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || body["ok"] != true {
		t.Fatalf("mọi thứ tốt: mong 200 ok:true, có %d %s", rec.Code, rec.Body)
	}
	for _, k := range []string{"workerHeartbeatAgeS", "ingestHeartbeatAgeS", "oldestPendingAgeS", "dead", "inDoubt", "spoolFiles"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("thiếu khóa %s: %s", k, rec.Body)
		}
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control %q", got)
	}
}
