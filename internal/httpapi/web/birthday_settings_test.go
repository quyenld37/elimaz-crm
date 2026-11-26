package web_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/cache"
)

const webBdSettingsPath = "/api/v1/web/birthdays/settings"

// bdSettings đọc cài đặt quà cho website bằng khóa key; trả mã, ETag và thân dạng map (để kiểm không có khóa thừa).
func (e *env) bdSettings(key string, hdr ...string) (int, string, map[string]any) {
	e.t.Helper()
	rec := e.get(key, webBdSettingsPath, hdr...)
	var m map[string]any
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			e.t.Fatalf("thân %q: %v", rec.Body.String(), err)
		}
	} else if rec.Code == http.StatusNotModified && rec.Body.Len() != 0 {
		e.t.Fatalf("304 có thân %q", rec.Body.String())
	}
	return rec.Code, rec.Header().Get("ETag"), m
}

// Cài đặt quà sinh nhật cho website (Task 512): chưa lưu → mặc định của CRM (tắt, 50.000 đ, 7, 14, 30, 0), đúng 6
// trường (không version, người sửa); ETag, If-None-Match khớp → 304 rỗng; hạn đệm 60 s; nhân viên lưu → website thấy số
// mới và ETag mới, ETag cũ → 200. Không khóa → 401; khóa thiếu web.catalog.read → 403.
func TestWebBirthdaySettings(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)

	code, tag, m := e.bdSettings(e.key)
	want := map[string]any{
		"enabled": false, "amount": 50000.0, "minOrder": 0.0, "daysBefore": 7.0, "validDays": 14.0, "minLeadDays": 30.0,
	}
	if code != http.StatusOK || !etagRe.MatchString(tag) || !maps.Equal(m, want) {
		t.Fatalf("mặc định: %d, ETag %q, %v", code, tag, m)
	}
	if ttl := e.rc.ttl(cache.SpaceBirthdaySettings); ttl != 60*time.Second {
		t.Fatalf("hạn đệm %s", ttl)
	}
	if c, _, _ := e.bdSettings(e.key, "If-None-Match", tag); c != http.StatusNotModified {
		t.Fatalf("If-None-Match khớp: %d", c)
	}

	in := promotion.BirthdaySettings{
		Enabled: true, Amount: 120000, MinOrder: 499000, DaysBefore: 3, ValidDays: 21, MinLeadDays: 45,
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, _, err := promotion.Writer{}.SaveBirthdaySettings(t.Context(), tx, in, 1,
			promotion.Actor{TenantID: e.tenant, UserID: e.owner.UserID}, time.Now())
		return err //nolint:wrapcheck // test
	})
	code, tag2, m := e.bdSettings(e.key, "If-None-Match", tag)
	want = map[string]any{
		"enabled": true, "amount": 120000.0, "minOrder": 499000.0, "daysBefore": 3.0, "validDays": 21.0, "minLeadDays": 45.0,
	}
	if code != http.StatusOK || tag2 == tag || !etagRe.MatchString(tag2) || !maps.Equal(m, want) {
		t.Fatalf("sau khi lưu (ETag cũ): %d, ETag %q (cũ %q), %v", code, tag2, tag, m)
	}

	for key, wantCode := range map[string]int{"": http.StatusUnauthorized, e.otherKey: http.StatusForbidden} {
		if c, _, _ := e.bdSettings(key); c != wantCode {
			t.Errorf("khóa %.6q: mong %d, có %d", key, wantCode, c)
		}
	}
}
