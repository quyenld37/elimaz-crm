package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	webBdImportPath = "/api/v1/web/birthdays/import"
	webBdGiftsPath  = "/api/v1/web/birthdays/gifts/"
)

type bdImportReport struct {
	Inserted, Skipped, Stopped int
	Conflicts                  []int
}

type pagedOutbox struct {
	outboxBody

	NextCursor string `json:"nextCursor"`
}

type giftFailure struct {
	Code     string `json:"code"`
	Failures int    `json:"failures"`
	RetryAt  string `json:"retryAt"`
	Given    bool   `json:"given"`
}

// bdImportItem là một hồ sơ sinh nhật cũ của website (18/12, Zalo, có đơn cũ, đăng ký 60 ngày trước now).
func bdImportItem(phone, token string, now time.Time, extra string) string {
	return fmt.Sprintf(`{"phone":%q,"fullName":"Khách cũ","day":18,"month":12,"zalo":true,"mail":false,`+
		`"stopToken":%q,"savedAt":%q,"legacyDelivered":true%s}`, phone, token,
		now.Add(-60*24*time.Hour).UTC().Format(time.RFC3339), extra)
}

// outboxPages đi hết outbox theo trang limit; trả mã theo thứ tự.
func (b *bdWeb) outboxPages(limit int) []string {
	b.t.Helper()
	var codes []string
	cursor := ""
	for range 10 {
		path := fmt.Sprintf("%s?limit=%d", webBdOutboxPath, limit)
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		p := decodeOK[pagedOutbox](b.t, "outbox", b.get(b.issueKey, path))
		for _, it := range p.Items {
			codes = append(codes, it.Code)
		}
		if p.NextCursor == "" {
			return codes
		}
		cursor = p.NextCursor
	}
	b.t.Fatal("outbox không hết trang")
	return nil
}

// [WB05] Website nhập hồ sơ sinh nhật cũ giữ mã dừng (webImportBirthdays): khóa thiếu web.birthdays.write → 403; 3 hồ
// sơ (một đã dừng) → inserted 3, stopped 1; nhập lại → skipped; mã dừng sai mẫu → 400. Lượt phát trong CRM → outbox 2
// quà, đi từng trang (limit 1, nextCursor) ra đủ 2 mã; quà lỗi (webBirthdayGiftFailed): failures 1, retryAt; gửi lại
// cùng khóa → câu trả lời cũ (không đếm hai lần); quà lỗi rời outbox, quà kia vẫn ra; khóa thiếu web.coupons.issue →
// 403; mã lạ → 404. Link dừng cũ (mã của website) dừng hồ sơ CRM → quà rời outbox. Log, audit không có SĐT, mã dừng.
func TestWebBirthday_ImportPagesFailed(t *testing.T) {
	t.Parallel()
	b := newBdWeb(t)
	tokens := []string{"WebTokenAAAAAAAAAAAAAAAA", "WebTokenBBBBBBBBBBBBBBBB", "WebTokenCCCCCCCCCCCCCCCC"}
	items := []string{
		bdImportItem("0912000501", tokens[0], b.now, ""),
		bdImportItem("0912000502", tokens[1], b.now, ""),
		bdImportItem("0912000503", tokens[2], b.now, `,"stoppedAt":"2026-10-01T00:00:00Z"`),
	}
	body := `{"items":[` + strings.Join(items, ",") + `]}`
	if rec := b.post(b.issueKey, "i0", webBdImportPath, body); rec.Code != http.StatusForbidden {
		t.Fatalf("khóa thiếu quyền: %d", rec.Code)
	}
	r := decodeOK[bdImportReport](t, "nhập", b.post(b.regKey, "i1", webBdImportPath, body))
	if r.Inserted != 3 || r.Stopped != 1 || len(r.Conflicts) != 0 {
		t.Fatalf("nhập: %+v", r)
	}
	if again := decodeOK[bdImportReport](t, "nhập lại", b.post(b.regKey, "i2", webBdImportPath, body)); again.Skipped != 3 ||
		again.Inserted != 0 {
		t.Fatalf("nhập lại: %+v", again)
	}
	bad := `{"items":[` + bdImportItem("0912000504", "ngan", b.now, "") + `]}`
	if rec := b.post(b.regKey, "i3", webBdImportPath, bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("mã dừng sai: %d %s", rec.Code, rec.Body)
	}

	b.runGifts(2)
	codes := b.outboxPages(1)
	if len(codes) != 2 || codes[0] == codes[1] {
		t.Fatalf("outbox theo trang: %v", codes)
	}
	b.checkGiftFailed(codes)

	for _, tok := range tokens[:2] {
		decodeOK[map[string]string](t, "link dừng cũ", b.post(b.regKey, "s-"+tok, webBdStopPath,
			fmt.Sprintf(`{"token":%q}`, tok)))
	}
	if got := b.outboxPages(100); len(got) != 0 {
		t.Fatalf("outbox sau khi dừng bằng link cũ: %v", got)
	}
	if n := b.staff.CountIn(t, b.tenant, `SELECT count(*) FROM identity.audit_logs
		WHERE row_to_json(audit_logs)::text LIKE '%91200050%' OR row_to_json(audit_logs)::text LIKE '%WebToken%'`); n != 0 {
		t.Errorf("audit chứa SĐT, mã dừng: %d", n)
	}
	logs := b.logs.String()
	for _, s := range []string{"91200050", "WebToken"} {
		if strings.Contains(logs, s) {
			t.Errorf("log chứa %q", s)
		}
	}
}

// checkGiftFailed: báo quà codes[0] lỗi → failures 1, retryAt; gửi lại cùng khóa → câu trả lời cũ; outbox còn codes[1];
// khóa thiếu quyền → 403; mã lạ → 404.
func (b *bdWeb) checkGiftFailed(codes []string) {
	t := b.t
	t.Helper()
	failed := func(key, idem, code string) int {
		return b.post(key, idem, webBdGiftsPath+code+"/failed", "").Code
	}
	f := decodeOK[giftFailure](t, "báo lỗi", b.post(b.issueKey, "f1", webBdGiftsPath+codes[0]+"/failed", ""))
	if f.Code != codes[0] || f.Failures != 1 || f.RetryAt == "" {
		t.Fatalf("báo lỗi: %+v", f)
	}
	if replay := decodeOK[giftFailure](t, "báo lỗi lại", b.post(b.issueKey, "f1", webBdGiftsPath+codes[0]+"/failed",
		"")); replay.Failures != 1 {
		t.Fatalf("gửi lại cùng khóa: %+v", replay)
	}
	if got := b.outboxPages(100); len(got) != 1 || got[0] != codes[1] {
		t.Fatalf("outbox sau khi quà lỗi: %v", got)
	}
	if code := failed(b.regKey, "f2", codes[1]); code != http.StatusForbidden {
		t.Fatalf("khóa thiếu quyền báo lỗi: %d", code)
	}
	if code := failed(b.issueKey, "f3", "KHONGCO-1"); code != http.StatusNotFound {
		t.Fatalf("mã lạ: %d", code)
	}
	if n := b.staff.CountIn(t, b.tenant, `SELECT count(*) FROM identity.audit_logs
		WHERE action = 'promotion.birthday_gift.send_failed' AND actor_type = 'service'`); n != 1 {
		t.Errorf("audit báo lỗi: %d", n)
	}
}
