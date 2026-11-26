package send_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// CH03: gửi chữ → queued, 1 lệnh chỉ mang messageId; worker → Meta nhận đúng 1 POST /messages có metadata =
// client_ref, RESPONSE, không tag; tin sent có mã của Meta; 2 báo hiệu (queued, sent); chạy lại worker không gửi thêm;
// hội thoại có mốc tin nhân viên, đoạn trích.
func TestSendText_CH03(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.mustSend(e.fbConv, send.Input{Text: textHello})
	if m.SendStatus != "queued" || m.Body != textHello || m.ClientRef == uuid.Nil {
		t.Fatalf("tin vừa gửi = %+v", m)
	}
	status, payload, _, _ := e.job(m.ID)
	if status != "pending" || len(payload) != 1 || payload["messageId"] != m.ID.String() {
		t.Fatalf("lệnh gửi = %s %v; payload chỉ được có messageId", status, payload)
	}
	if n := e.signals(e.fbConv); n != 1 {
		t.Fatalf("báo hiệu sau khi bấm gửi = %d, muốn 1", n)
	}
	e.drain()
	e.drain()
	if n := len(e.fbCalls()); n != 1 {
		t.Fatalf("Meta nhận %d lần gửi, muốn 1", n)
	}
	b := e.body(0)
	msgBody, _ := b["message"].(map[string]any)
	if msgBody["metadata"] != m.ClientRef.String() || msgBody["text"] != textHello || b["messaging_type"] != "RESPONSE" ||
		b["tag"] != nil {
		t.Fatalf("thân gửi Meta = %v", b)
	}
	if r, _ := b["recipient"].(map[string]any); r["id"] != psid {
		t.Fatalf("người nhận = %v", b["recipient"])
	}
	got := e.message(m.ID)
	if got.Status != "sent" || !strings.HasPrefix(got.ExternalID, "m_") || got.Origin != "staff" {
		t.Fatalf("tin sau khi gửi = %+v", got)
	}
	if n := e.signals(e.fbConv); n != 2 {
		t.Fatalf("báo hiệu = %d, muốn 2 (queued, sent)", n)
	}
	if n := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM conversation.conversations WHERE id = $1 AND last_staff_message_at IS NOT NULL
		AND last_snippet = $2`, e.fbConv, textHello); n != 1 {
		t.Fatal("hội thoại chưa cập nhật mốc tin nhân viên, đoạn trích")
	}
}

// addImage chèn (chủ bảng) một tệp mục đích purpose trạng thái status của tenant tenant, kèm biến thể JPEG 640, 1280
// và WebP 2048 của bộ v1 trên bucket public; trả mã tệp, mã băm.
func (e *env) addImage(tenant uuid.UUID, purpose, status string) (uuid.UUID, string) {
	e.t.Helper()
	id, sha := uuid.New(), strings.Repeat("ab", 32)
	ctx := e.t.Context()
	e.staff.Exec(e.t, tenant, func(tx pgx.Tx) error {
		reject := any(nil)
		if status == "rejected" {
			reject = "not_image"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO platform.files (id, tenant_id, purpose, bucket, key, original_name, declared_type,
			size_bytes, content_sha256, status, reject_reason, preset_version)
			VALUES ($1::uuid, $2, $3, 'originals', 'tmp/' || $1::text, 'a.jpg', 'image/jpeg', 10, decode($4, 'hex'), $5, $6, 1)`,
			id, tenant, purpose, sha, status, reject); err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, v := range []struct {
			w      int
			format string
		}{{640, "jpeg"}, {1280, "jpeg"}, {2048, "webp"}} {
			if _, err := tx.Exec(ctx, `INSERT INTO platform.file_variants (tenant_id, file_id, preset_version, width, format,
				bucket, key, size_bytes) VALUES ($1, $2, 1, $3::int, $4::text, 'public', 'img/' || $5::text || '/v1/' || $3::int::text || '.' || $4::text, 1)`,
				tenant, id, v.w, v.format, sha); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	return id, sha
}

// CH04: ảnh chat_outgoing ready → thân gửi Meta có attachment.payload.url là biến thể JPEG lớn nhất trên
// FILES_PUBLIC_BASE_URL, không có chữ; ảnh đang xử lý → ErrAttachmentNotReady; mục đích khác hay bị từ chối →
// ErrAttachmentRejected; tệp của tenant khác → không có; cả chữ lẫn ảnh, không gì, chữ toàn khoảng trắng hay quá 2 000
// ký tự → ErrInvalidInput. Không ca lỗi nào ghi tin hay lệnh.
func TestSendImage_CH04(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	img, sha := e.addImage(e.tenant, send.OutgoingImagePurpose, "ready")
	m := e.mustSend(e.fbConv, send.Input{AttachmentFileID: &img})
	if len(m.Attachments) != 1 || *m.Attachments[0].FileID != img || m.Attachments[0].Type != "image" {
		t.Fatalf("đính kèm = %+v", m.Attachments)
	}
	e.drain()
	b := e.body(0)
	msgBody, _ := b["message"].(map[string]any)
	att, _ := msgBody["attachment"].(map[string]any)
	pl, _ := att["payload"].(map[string]any)
	if want := filesBase + "/img/" + sha + "/v1/1280.jpeg"; pl["url"] != want || att["type"] != "image" || msgBody["text"] != nil {
		t.Fatalf("thân gửi Meta = %v, muốn url %s", b, want)
	}
	if got := e.message(m.ID); got.Status != "sent" {
		t.Fatalf("tin ảnh = %+v", got)
	}
	if n := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM conversation.conversations WHERE id = $1 AND last_snippet = '[Tệp đính kèm]'`, e.fbConv); n != 1 {
		t.Fatal("đoạn trích của tin ảnh sai")
	}

	processing, _ := e.addImage(e.tenant, send.OutgoingImagePurpose, "processing")
	product, _ := e.addImage(e.tenant, "product_image", "ready")
	rejected, _ := e.addImage(e.tenant, send.OutgoingImagePurpose, "rejected")
	other := e.staff.Tenant(t, "B")
	foreign, _ := e.addImage(other, send.OutgoingImagePurpose, "ready")
	long := strings.Repeat("a", 2001)
	before := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM conversation.messages`)
	for _, c := range []struct {
		name string
		in   send.Input
		want error
	}{
		{"đang xử lý", send.Input{AttachmentFileID: &processing}, send.ErrAttachmentNotReady},
		{"ảnh sản phẩm", send.Input{AttachmentFileID: &product}, send.ErrAttachmentRejected},
		{"bị từ chối", send.Input{AttachmentFileID: &rejected}, send.ErrAttachmentRejected},
		{"tenant khác", send.Input{AttachmentFileID: &foreign}, access.ErrNotFound},
		{"chữ và ảnh", send.Input{Text: "x", AttachmentFileID: &img}, send.ErrInvalidInput},
		{"không gì", send.Input{}, send.ErrInvalidInput},
		{"khoảng trắng", send.Input{Text: "   "}, send.ErrInvalidInput},
		{"quá dài", send.Input{Text: long}, send.ErrInvalidInput},
	} {
		if _, err := e.send(e.fbConv, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: lỗi = %v, muốn %v", c.name, err, c.want)
		}
	}
	if after := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM conversation.messages`); after != before {
		t.Fatalf("ca lỗi đã ghi %d tin", after-before)
	}
}

// CH09: tin cuối của khách cách 25 giờ → *NotAllowedError window_closed, không tin, không lệnh; Page bật Human Agent,
// cách 3 ngày: có humanAgent → gửi với tag HUMAN_AGENT; thiếu → window_closed kèm hạn 7 ngày. Không quyền trên Page →
// không có (404); hội thoại không có → không có.
func TestSendPolicy_CH09(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	old := e.conversation(e.fbPage, "psid-1", time.Now().Add(-25*time.Hour))
	_, err := e.send(old, send.Input{Text: textHello})
	var na *send.NotAllowedError
	if !errors.As(err, &na) || na.Reason != channel.DenyWindowClosed {
		t.Fatalf("25 giờ: lỗi = %v, muốn window_closed", err)
	}
	if n := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE conversation_id = $1`, old); n != 0 {
		t.Fatalf("ngoài cửa sổ vẫn ghi %d tin", n)
	}

	e.exec(`UPDATE channel.channel_accounts SET human_agent_enabled = true WHERE id = $1`, e.fbPage)
	lastIn := time.Now().Add(-72 * time.Hour).UTC().Truncate(time.Microsecond)
	agent := e.conversation(e.fbPage, "psid-2", lastIn)
	_, err = e.send(agent, send.Input{Text: textHello})
	if !errors.As(err, &na) || na.Reason != channel.DenyWindowClosed || na.Until == nil ||
		!na.Until.Equal(lastIn.Add(7*24*time.Hour)) {
		t.Fatalf("3 ngày, thiếu humanAgent: lỗi = %v %+v", err, na)
	}
	m := e.mustSend(agent, send.Input{Text: textHello, HumanAgent: true})
	e.drain()
	if b := e.body(0); b["tag"] != "HUMAN_AGENT" || b["messaging_type"] != "MESSAGE_TAG" {
		t.Fatalf("thân gửi Human Agent = %v", b)
	}
	if got := e.message(m.ID); got.Status != "sent" {
		t.Fatalf("tin Human Agent = %+v", got)
	}

	stranger := e.user
	stranger.Grants = nil
	e.user = stranger
	if _, err := e.send(e.fbConv, send.Input{Text: "x"}); !errors.Is(err, access.ErrNotFound) {
		t.Fatalf("không quyền: lỗi = %v", err)
	}
	if _, err := e.send(uuid.New(), send.Input{Text: "x"}); !errors.Is(err, access.ErrNotFound) {
		t.Fatalf("hội thoại không có: lỗi = %v", err)
	}
}

// MO03: Page chỉ xem → ErrReadOnly, không tin; tin đã ghi lệnh rồi Page mới chuyển chỉ xem → lệnh hỏi lại chính sách,
// tin failed lý do read_only, Meta không nhận gì.
func TestSendReadOnly_MO03(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.mustSend(e.fbConv, send.Input{Text: textHello})
	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'read_only' WHERE id = $1`, e.fbPage)
	if _, err := e.send(e.fbConv, send.Input{Text: "x"}); !errors.Is(err, send.ErrReadOnly) {
		t.Fatalf("Page chỉ xem: lỗi = %v", err)
	}
	e.drain()
	if n := len(e.fbCalls()); n != 0 {
		t.Fatalf("Meta nhận %d lần gửi", n)
	}
	got := e.message(m.ID)
	if got.Status != "failed" || got.FailReason != "read_only" {
		t.Fatalf("tin = %+v, muốn failed read_only", got)
	}
	if status, _, _, _ := e.job(m.ID); status != "dead" {
		t.Fatalf("lệnh = %s, muốn dead", status)
	}
	if send.FailReasonText(got.FailReason) == send.FailReasonText("mã-lạ") {
		t.Fatal("lý do read_only không có câu tiếng Việt riêng")
	}
}

// Lỗi Meta: 551 → failed blocked_by_user, hội thoại bị chặn, lần gửi sau bị từ chối ngay; 190 → failed auth, Page
// degraded, đúng 1 lệnh cảnh báo gấp; 613 → lệnh pending hẹn ≥ 60 s, tin còn queued (lý do rate_limited), last_error
// không có nội dung tin.
func TestSendMetaErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	path := fbPageExt + "/messages"

	e.fg.FailNext(path, fakegraph.GraphErrorSpec{Code: 551, Type: "OAuthException", Message: "This person isn't available"})
	blocked := e.mustSend(e.fbConv, send.Input{Text: textHello})
	e.drain()
	if got := e.message(blocked.ID); got.Status != "failed" || got.FailReason != "blocked_by_user" {
		t.Fatalf("551: tin = %+v", got)
	}
	_, err := e.send(e.fbConv, send.Input{Text: "x"})
	var na *send.NotAllowedError
	if !errors.As(err, &na) || na.Reason != channel.DenyBlockedByUser {
		t.Fatalf("sau 551: lỗi = %v", err)
	}

	conv2 := e.conversation(e.fbPage, "psid-3", time.Now().Add(-time.Hour))
	e.fg.FailNext(path, fakegraph.GraphErrorSpec{Code: 190, Type: "OAuthException", Message: "Error validating access token"})
	auth := e.mustSend(conv2, send.Input{Text: textHello})
	e.drain()
	if got := e.message(auth.ID); got.Status != "failed" || got.FailReason != "auth" {
		t.Fatalf("190: tin = %+v", got)
	}
	if n := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM channel.channel_accounts WHERE id = $1 AND status = 'degraded'
		AND status_reason = $2`, e.fbPage, channel.ReasonTokenInvalid); n != 1 {
		t.Fatal("190: Page chưa degraded")
	}
	if n := e.alerts(); n != 1 {
		t.Fatalf("190: %d lệnh cảnh báo, muốn 1", n)
	}

	conv3 := e.conversation(e.fbPage, "psid-4", time.Now().Add(-time.Hour))
	e.fg.FailNext(path, fakegraph.GraphErrorSpec{Code: 613, Type: "OAuthException", Message: "Calls to this api have exceeded the rate limit"})
	limited := e.mustSend(conv3, send.Input{Text: textHello})
	e.drain()
	status, _, lastErr, inSec := e.job(limited.ID)
	if status != "pending" || inSec < 55 {
		t.Fatalf("613: lệnh %s hẹn sau %.0f s, muốn pending ≥ 60 s", status, inSec)
	}
	if strings.Contains(lastErr, "shop") || strings.Contains(lastErr, "size") {
		t.Fatalf("last_error chứa nội dung tin: %q", lastErr)
	}
	if got := e.message(limited.ID); got.Status != "queued" || got.FailReason != "rate_limited" {
		t.Fatalf("613: tin = %+v", got)
	}
}

// alerts đếm lệnh cảnh báo token Page hỏng.
func (e *env) alerts() int {
	var n int
	if err := e.staff.Owner.Pool.QueryRow(e.t.Context(), `SELECT count(*) FROM platform.jobs WHERE kind = $1`,
		channel.AuthAlertKind).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// Giới hạn tốc độ (04 mục 5.3): thùng 50 lượt, nạp 20/giây, đồng hồ đứng: 60 tin → Meta nhận đúng 50, 10 tin còn
// queued (không tiêu lượt thử); tiến đồng hồ 1 giây → cả 60 tin sent, Meta nhận 60.
func TestSendRateLimit(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.clock.Freeze(time.Now().UTC())
	ids := make([]uuid.UUID, 0, 60)
	for range 60 {
		ids = append(ids, e.mustSend(e.fbConv, send.Input{Text: textHello}).ID)
	}
	for range 12 {
		testkit.RunRound(t, e.staff.Owner, e.worker, 10)
	}
	if n := len(e.fbCalls()); n != 50 {
		t.Fatalf("đồng hồ đứng: Meta nhận %d lần, muốn 50", n)
	}
	if n := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE send_status = 'queued'`); n != 10 {
		t.Fatalf("%d tin còn queued, muốn 10", n)
	}
	e.clock.Freeze(e.clock.Now().Add(time.Second))
	e.staff.DrainJobs(t, e.worker)
	if n := len(e.fbCalls()); n != 60 {
		t.Fatalf("sau 1 giây: Meta nhận %d lần, muốn 60", n)
	}
	for _, id := range ids {
		if got := e.message(id); got.Status != "sent" {
			t.Fatalf("tin %s = %+v", id, got)
		}
	}
}

// Meta báo mức dùng ≥ 80% khi gửi → tin vẫn sent, Page bị giãn gửi (blocked_until ở tương lai): AcquireSendToken từ
// chối tới mốc đó (02 mục 6).
func TestSendSlowDown(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fg.SetUsagePct(85)
	m := e.mustSend(e.fbConv, send.Input{Text: textHello})
	e.drain()
	if got := e.message(m.ID); got.Status != "sent" {
		t.Fatalf("tin = %+v", got)
	}
	if n := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM channel.channel_accounts WHERE id = $1 AND blocked_until > $2`,
		e.fbPage, time.Now().UTC().Add(-time.Second)); n != 1 {
		t.Fatal("Page chưa được giãn gửi sau khi Meta báo mức dùng cao")
	}
}
