package inbound_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/history"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/inbound"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// mutAttachmentSnippet là đoạn trích của tin chỉ có tệp (deps.Snippet với nội dung rỗng).
var mutAttachmentSnippet = deps.Snippet("", true) //nolint:gochecknoglobals // hằng tính từ deps

// mutCommand trả lệnh loại kind trong Parts(d).
//
//nolint:ireturn // lệnh của Parts là interface commands.Handler; test gọi Execute, OnDone, OnTerminal qua nó
func mutCommand(t *testing.T, d deps.Deps, kind string) commands.Handler {
	t.Helper()
	for _, c := range inbound.Parts(d).Commands {
		if c.Kind().Name == kind {
			return c
		}
	}
	t.Fatalf("không có lệnh %s", kind)
	return nil
}

// mutMessageID đọc mã tin có mã ngoài mid ở tenant A của env.
func (e *env) mutMessageID(mid string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id FROM conversation.messages WHERE external_id = $1`, mid).Scan(&id)
	})
	return id
}

// mutStatuses đọc trạng thái các mục đính kèm của tin id.
func (e *env) mutStatuses(id uuid.UUID) []string {
	e.t.Helper()
	return e.texts(`SELECT a->>'status' FROM conversation.messages m, jsonb_array_elements(m.attachments) a
		WHERE m.id = $1 ORDER BY (a->>'index')::int`, id)
}

// mutFetchJobs đếm lệnh tải tệp của tin id.
func (e *env) mutFetchJobs(id uuid.UUID) int {
	e.t.Helper()
	return e.staff.Count(e.t, `SELECT count(*) FROM platform.jobs WHERE kind = $1 AND payload->>'messageId' = $2`,
		deps.FetchAttachmentKind().Name, id.String())
}

// mutInboundNotDone đếm việc vào của nguồn facebook chưa xong (không done, không dead).
func (e *env) mutInboundNotDone() int {
	e.t.Helper()
	return e.staff.Count(e.t, `SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.facebook' AND status NOT IN ('done', 'dead')`)
}

// mutInboundDead đếm việc vào của nguồn facebook đã dead.
func (e *env) mutInboundDead() int {
	e.t.Helper()
	return e.staff.Count(e.t, `SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.facebook' AND status = 'dead'`)
}

// mutMetric là tổng đếm của chỉ số name nhãn label trong rows.
func mutMetric(rows []metrics.Row, name, label string) int64 {
	total := int64(0)
	for i := range rows {
		if rows[i].Name != name || rows[i].Label != label {
			continue // chỉ số hay nhãn khác
		}
		total += rows[i].Count
	}
	return total
}

// Lệnh tải tệp gọi thẳng: payload thiếu mã tin → rejected bad_payload; chỉ số mục âm hay vượt số mục → xong, không
// làm gì (không đọc ngoài mảng). OnDone với kết quả rỗng ({}: tin hay mục không còn) → không ghi gì; OnTerminal:
// không tenant → bỏ qua, in_doubt giữ pending, dead → failed, expired → expired, mục đã xong → giữ nguyên, không lỗi;
// lỗi database khi ghi mục → trả lỗi. Mục không URL → lệnh không làm gì (Task 182, mutation testing M4).
func TestMutM4_FetchAttachmentDirect(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	p := message("u-att", "mid-att", "", 0)
	p.Attachments = []channel.AttachmentRef{
		{Type: "image", URL: "https://scontent.fbcdn.net/a1.jpg"},
		{Type: "image", URL: "https://scontent.fbcdn.net/a2.jpg"},
		{Type: "fallback"}, // mục không URL: giữ pending, không lệnh
	}
	e.deliver(channel.EventMessageReceived, "m:mid-att", "u-att", at(0), p)
	id := e.mutMessageID("mid-att")
	h := mutCommand(t, e.deps, deps.FetchAttachmentKind().Name)
	job := func(payload string, tenant *uuid.UUID) queue.Job {
		return queue.Job{ID: uuid.New(), TenantID: tenant, Kind: deps.FetchAttachmentKind().Name, Payload: json.RawMessage(payload), Attempts: 1}
	}
	pl := func(i int) string { return `{"messageId":"` + id.String() + `","index":` + strconv.Itoa(i) + `}` }

	if out, err := h.Execute(t.Context(), job(`{}`, &e.tenant)); err != nil || out.Status != commands.StatusRejected || out.Reason != "bad_payload" {
		t.Fatalf("payload thiếu mã tin: %+v, %v; muốn rejected bad_payload", out, err)
	}
	for _, payload := range []string{`{"messageId":"` + id.String() + `","index":-1}`, pl(2), pl(3)} {
		out, err := h.Execute(t.Context(), job(payload, &e.tenant))
		if err != nil || out.Status != commands.StatusDone || string(out.Result) != `{}` {
			t.Fatalf("chỉ số ngoài mảng hay mục không URL %s: %+v, %v; muốn done {}", payload, out, err)
		}
	}

	done, ok := h.(commands.DoneHook)
	term, ok2 := h.(queue.TerminalHook)
	if !ok || !ok2 {
		t.Fatal("lệnh tải phải có OnDone và OnTerminal")
	}
	run := func(fn func(tx pgx.Tx) error) error {
		var got error
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error { got = fn(tx); return nil })
		return got
	}
	if err := run(func(tx pgx.Tx) error {
		return done.OnDone(t.Context(), job(pl(0), &e.tenant), json.RawMessage(`{}`), tx)
	}); err != nil {
		t.Fatalf("OnDone kết quả rỗng: %v", err)
	}
	if err := run(func(tx pgx.Tx) error { return term.OnTerminal(t.Context(), job(pl(0), nil), queue.StatusDead, tx) }); err != nil {
		t.Fatalf("OnTerminal không tenant: %v", err)
	}
	if err := run(func(tx pgx.Tx) error {
		return term.OnTerminal(t.Context(), job(pl(0), &e.tenant), queue.StatusInDoubt, tx)
	}); err != nil {
		t.Fatalf("OnTerminal in_doubt: %v", err)
	}
	if got := e.mutStatuses(id); len(got) != 3 || got[0] != "pending" || got[1] != "pending" || got[2] != "pending" {
		t.Fatalf("sau OnDone rỗng, không tenant, in_doubt: %v; muốn ba mục pending", got)
	}
	if err := run(func(tx pgx.Tx) error {
		return term.OnTerminal(t.Context(), job(pl(0), &e.tenant), queue.StatusDead, tx)
	}); err != nil {
		t.Fatalf("OnTerminal dead: %v", err)
	}
	if err := run(func(tx pgx.Tx) error {
		return term.OnTerminal(t.Context(), job(pl(1), &e.tenant), queue.StatusExpired, tx)
	}); err != nil {
		t.Fatalf("OnTerminal expired: %v", err)
	}
	if err := run(func(tx pgx.Tx) error {
		return term.OnTerminal(t.Context(), job(pl(0), &e.tenant), queue.StatusExpired, tx)
	}); err != nil {
		t.Fatalf("OnTerminal trên mục đã failed: %v; muốn nil", err)
	}
	if got := e.mutStatuses(id); len(got) != 3 || got[0] != "failed" || got[1] != "expired" || got[2] != "pending" {
		t.Fatalf("sau dead, expired: %v; muốn failed, expired, pending", got)
	}
	// Lỗi database khi ghi mục (transaction đã hỏng) → OnTerminal trả lỗi, không nuốt.
	var aborted error
	_ = e.staff.TryExec(t.Context(), e.tenant, func(tx pgx.Tx) error {
		_, _ = tx.Exec(t.Context(), `SELECT 1/0`)
		aborted = term.OnTerminal(t.Context(), job(pl(2), &e.tenant), queue.StatusDead, tx)
		return nil
	})
	if aborted == nil {
		t.Fatal("OnTerminal trên transaction hỏng: nil, muốn lỗi")
	}
}

// Tệp khách gửi (PDF) lưu mã băm SHA-256 của nội dung và tên tep-<8 ký tự đầu mã băm>.pdf; ảnh đại diện từ hồ sơ kênh
// cũng lưu mã băm (Task 182, mutation testing M4).
func TestMutM4_FileHashAndName(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	body := []byte("%PDF-1.7\nnoi dung thu\n")
	avatar := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	e.chat.SetProfile("u1", channel.Profile{Name: "Lan", AvatarURL: e.chat.AddFile("/a/u1.png", avatar)})
	e.receive(e.tenant, "u1", "m-pdf", file(e.chat.AddFile("/f/hash.pdf", body)))
	it, _ := e.one("m-pdf")
	if it.FileID == nil {
		t.Fatalf("mục: %+v", it)
	}
	sum := sha256.Sum256(body)
	var name string
	var stored []byte
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT original_name, content_sha256 FROM platform.files WHERE id = $1`, *it.FileID).
			Scan(&name, &stored)
	})
	if want := "tep-" + hex.EncodeToString(sum[:])[:8] + ".pdf"; name != want || hex.EncodeToString(stored) != hex.EncodeToString(sum[:]) {
		t.Fatalf("tệp: tên %q mã băm %x; muốn %q, %x", name, stored, want, sum)
	}
	_, _, av := e.profileOf("m-pdf")
	if av == nil {
		t.Fatal("không có ảnh đại diện")
	}
	asum := sha256.Sum256(avatar)
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.files WHERE id = $1 AND content_sha256 = $2`, *av, asum[:]); n != 1 {
		t.Fatal("ảnh đại diện không lưu mã băm")
	}
}

// Lệnh đọc hồ sơ gọi thẳng: payload thiếu mã hội thoại → rejected bad_payload; khách trong payload không còn → xong
// (bỏ qua tên, không thử lại); không có kho tệp → xong, không ảnh; OnDone không tenant → không làm gì (Task 182, mutation testing M4).
func TestMutM4_FetchProfileDirect(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	e.chat.SetProfile("u1", channel.Profile{Name: "Lan"})
	e.receive(e.tenant, "u1", "m-1")
	_, conv := e.items(e.tenant, "m-1")
	reg, err := channel.NewRegistry(e.chat)
	if err != nil {
		t.Fatal(err)
	}
	kr, err := fieldcrypt.ParseKeyring(testKeyring)
	if err != nil {
		t.Fatal(err)
	}
	d := deps.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Bus: e.bus, Events: events.NewRegistry(), Registry: reg, Keys: kr,
		Files: e.store, Metrics: metrics.NewRegistry(),
	}
	h := mutCommand(t, d, inbound.FetchProfileKind().Name)
	job := func(payload string, tenant *uuid.UUID) queue.Job {
		return queue.Job{ID: uuid.New(), TenantID: tenant, Kind: inbound.FetchProfileKind().Name, Payload: json.RawMessage(payload), Attempts: 1}
	}
	if out, err := h.Execute(t.Context(), job(`{}`, &e.tenant)); err != nil || out.Status != commands.StatusRejected || out.Reason != "bad_payload" {
		t.Fatalf("payload thiếu hội thoại: %+v, %v", out, err)
	}
	gone := `{"customerId":"` + uuid.New().String() + `","conversationId":"` + conv.String() + `"}`
	if out, err := h.Execute(t.Context(), job(gone, &e.tenant)); err != nil || out.Status != commands.StatusDone {
		t.Fatalf("khách không còn: %+v, %v; muốn done", out, err)
	}
	// Không có kho tệp: bỏ qua ảnh đại diện, vẫn xong (không ghi lên kho).
	e.chat.SetProfile("u1", channel.Profile{Name: "Lan", AvatarURL: e.chat.AddFile("/a/khong-kho.png",
		append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...))})
	noStore := d
	noStore.Files = nil
	full := `{"customerId":"` + uuid.Nil.String() + `","conversationId":"` + conv.String() + `"}`
	if out, err := mutCommand(t, noStore, inbound.FetchProfileKind().Name).Execute(t.Context(), job(full, &e.tenant)); err != nil ||
		out.Status != commands.StatusDone || string(out.Result) != `{}` {
		t.Fatalf("không kho tệp: %+v, %v; muốn done không ảnh", out, err)
	}
	done, ok := h.(commands.DoneHook)
	if !ok {
		t.Fatal("lệnh hồ sơ phải có OnDone")
	}
	var got error
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		got = done.OnDone(t.Context(), job(gone, nil), json.RawMessage(`{}`), tx)
		return nil
	})
	if got != nil {
		t.Fatalf("OnDone không tenant: %v", got)
	}
}

// Lệnh đọc bài gọi thẳng: payload thiếu mã tài khoản hay mã bài → rejected bad_payload (không đọc tài khoản); Page của kênh không
// đọc được bài (kênh giả, có token) → rejected no_post_fetcher (Task 182, mutation testing M4).
func TestMutM4_FetchPostDirect(t *testing.T) {
	t.Parallel()
	e := newEnvWith(t, fakegraphFor(t))
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return channel.StoreCredentials(t.Context(), tx, e.deps.Keys, e.fakePage, logging.Secret("tok-gia"), "page_user")
	})
	h := mutCommand(t, e.deps, inbound.FetchPostKind)
	job := func(payload string) queue.Job {
		return queue.Job{ID: uuid.New(), TenantID: &e.tenant, Kind: inbound.FetchPostKind, Payload: json.RawMessage(payload), Attempts: 1}
	}
	if out, err := h.Execute(t.Context(), job(`{"externalId":"x"}`)); err != nil || out.Status != commands.StatusRejected || out.Reason != "bad_payload" {
		t.Fatalf("payload thiếu tài khoản: %+v, %v; muốn rejected bad_payload", out, err)
	}
	noExt := `{"accountId":"` + e.page1.String() + `","postId":"` + uuid.New().String() + `","externalId":""}`
	if out, err := h.Execute(t.Context(), job(noExt)); err != nil || out.Status != commands.StatusRejected || out.Reason != "bad_payload" {
		t.Fatalf("payload thiếu mã bài: %+v, %v; muốn rejected bad_payload", out, err)
	}
	fk := `{"accountId":"` + e.fakePage.String() + `","postId":"` + uuid.New().String() + `","externalId":"post-1"}`
	if out, err := h.Execute(t.Context(), job(fk)); err != nil || out.Status != commands.StatusRejected || out.Reason != "no_post_fetcher" {
		t.Fatalf("kênh không đọc được bài: %+v, %v; muốn rejected no_post_fetcher", out, err)
	}
}

// Ảnh bài lưu mã băm SHA-256 của nội dung và tên bai-<8 ký tự đầu mã băm>.png (Task 182, mutation testing M4).
func TestMutM4_PostImageHash(t *testing.T) {
	t.Parallel()
	conversation.RegisterFilePurposes()
	fg := fakegraphFor(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("anh-bai-mut")...)
	p := fakePost(post1, "Bài có ảnh")
	p.PictureURL = "https://scontent.fbcdn.net/mut.png"
	fg.AddPost(p)
	e := newEnvWith(t, fg, withCDN(t, map[string][]byte{p.PictureURL: png}))
	e.sendComment(commentEv("add", post1, "c1", "", "u-7", "Lan", "Chốt", 0))
	e.drain()
	sum := sha256.Sum256(png)
	want := "bai-" + hex.EncodeToString(sum[:])[:8] + ".png"
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.files WHERE purpose = 'chat_post_image'
		AND content_sha256 = $1 AND original_name = $2`, sum[:], want); n != 1 {
		t.Fatalf("ảnh bài có mã băm, tên %s = %d, muốn 1", want, n)
	}
}

// Sự kiện của Page không có trong shop (echo, comment, đã nhận, cảm xúc, sửa tin) → việc xong, không ghi tin, chỉ
// đếm inbound_ignored{account_missing}, không đếm lý do bỏ qua của bước sau. Comment thiếu mã comment hay mã bài →
// việc dead; comment gốc của Page (không có hội thoại) → việc xong (Task 182, mutation testing M4).
func TestMutM4_UnknownPageAndCommentGuards(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	const lost = "104999999999999"
	e.reg.Drain()
	e.enqueue(facebook.Key, channel.EventMessageEcho, "x:echo", lost, "u-1", at(0), channel.MessagePayload{
		PageID: lost, UserID: "u-1", MID: "mid-x1", Text: "chào", IsEcho: true, EchoAppID: businessSuiteAppID, SentAt: at(0),
	})
	e.enqueue(facebook.Key, channel.EventCommentChanged, "x:cmt", lost, "u-2", at(0), channel.CommentPayload{
		PageID: lost, PostID: lost + "_1", CommentID: "cx1", ParentID: lost + "_1", FromID: "u-2", FromName: "Lan",
		Text: "hỏi", Verb: "add", CreatedAt: at(0),
	})
	e.enqueue(facebook.Key, channel.EventMessageDelivered, "x:wm", lost, "u-3", at(0),
		channel.WatermarkPayload{PageID: lost, UserID: "u-3", Watermark: at(0)})
	e.enqueue(facebook.Key, channel.EventMessageReaction, "x:re", lost, "u-4", at(0),
		channel.ReactionPayload{PageID: lost, UserID: "u-4", MID: "mid-x", Reaction: "love", Action: "react"})
	e.enqueue(facebook.Key, channel.EventMessageEdited, "x:ed", lost, "u-5", at(0),
		channel.EditPayload{PageID: lost, UserID: "u-5", MID: "mid-x", Text: "sửa", EditedAt: at(0)})
	e.drain()
	rows := e.reg.Drain()
	if n := e.mutInboundNotDone(); n != 0 {
		t.Fatalf("%d việc vào chưa xong cho Page lạ", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages`); n != 0 {
		t.Fatalf("%d tin cho Page lạ", n)
	}
	if n := mutMetric(rows, "inbound_ignored", "account_missing"); n != 5 {
		t.Errorf("account_missing = %d, muốn 5", n)
	}
	for _, l := range []string{"watermark_unknown_conversation", "reaction_unknown_message", "edit_unknown_message"} {
		if n := mutMetric(rows, "inbound_ignored", l); n != 0 {
			t.Errorf("%s = %d, muốn 0 (Page lạ dừng ở bước tìm Page)", l, n)
		}
	}

	noID := commentEv("add", post1, "", "", "u-6", "Hoa", "thiếu mã comment", 1)
	e.enqueue(facebook.Key, channel.EventCommentChanged, "x:noid", pageExt1, post1+":u-6", at(1), noID)
	noPost := commentEv("add", "", "c-nopost", "", "u-8", "Hoa", "thiếu mã bài", 2)
	noPost.ParentID = "x"
	e.enqueue(facebook.Key, channel.EventCommentChanged, "x:nopost", pageExt1, ":u-8", at(2), noPost)
	e.enqueue(facebook.Key, channel.EventCommentChanged, "x:root", pageExt1, post1+":"+pageExt1, at(3),
		commentEv("add", post1, "c-root", "", pageExt1, "Shop", "Bài mới đây", 3))
	e.drain()
	if n := e.mutInboundDead(); n != 2 {
		t.Errorf("việc dead = %d, muốn 2 (thiếu mã comment, thiếu mã bài)", n)
	}
	if n := e.mutInboundNotDone(); n != 0 {
		t.Errorf("%d việc vào chưa xong (comment gốc của Page phải xong)", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages`); n != 0 {
		t.Errorf("%d tin, muốn 0", n)
	}
}

// Echo có ảnh: Business Suite (page_external) → mục giữ URL, một lệnh tải, đoạn trích "[Tệp đính kèm]"; echo của CRM
// không khớp (own_echo_unmatched) → không lệnh tải. Echo không nội dung, không tệp → không đoạn trích (Task 182,
// mutation testing M4).
func TestMutM4_EchoAttachments(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ref := []channel.AttachmentRef{{Type: "image", URL: "https://scontent.fbcdn.net/echo.jpg"}}
	e.deliver(channel.EventMessageEcho, "m:mid-bs", "u-1", at(0), channel.MessagePayload{
		PageID: pageExt1, UserID: "u-1", MID: "mid-bs", IsEcho: true, EchoAppID: businessSuiteAppID, SentAt: at(0), Attachments: ref,
	})
	e.deliver(channel.EventMessageEcho, "m:mid-own", "u-2", at(0), channel.MessagePayload{
		PageID: pageExt1, UserID: "u-2", MID: "mid-own", IsEcho: true, EchoAppID: ownAppID, SentAt: at(0), Attachments: ref,
	})
	e.deliver(channel.EventMessageEcho, "m:mid-empty", "u-3", at(0), channel.MessagePayload{
		PageID: pageExt1, UserID: "u-3", MID: "mid-empty", IsEcho: true, EchoAppID: businessSuiteAppID, SentAt: at(0),
	})
	e.noDeadJobs()
	if n := e.mutFetchJobs(e.mutMessageID("mid-bs")); n != 1 {
		t.Errorf("lệnh tải của echo Business Suite = %d, muốn 1", n)
	}
	if n := e.mutFetchJobs(e.mutMessageID("mid-own")); n != 0 {
		t.Errorf("lệnh tải của echo CRM không khớp = %d, muốn 0", n)
	}
	if c := e.conversation(e.page1, "u-1"); deref(c.Snippet) != mutAttachmentSnippet {
		t.Errorf("đoạn trích echo có ảnh = %q, muốn %q", deref(c.Snippet), mutAttachmentSnippet)
	}
	if c := e.conversation(e.page1, "u-3"); c.Snippet != nil {
		t.Errorf("đoạn trích echo rỗng = %q, muốn không có", *c.Snippet)
	}
}

// Echo của CRM khớp tin đã bị nhân viên bỏ (discarded) → late_echo_after_discard + 1; khớp tin queued → không đếm;
// echo không mid → không khớp tin nào (Task 182, mutation testing M4).
func TestMutM4_LateEchoMetric(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.receive("u-1", "mid-in", "còn hàng không", 0)
	c := e.conversation(e.page1, "u-1")
	e.reg.Drain()
	for i, status := range []string{"queued", "discarded"} {
		r := uuid.New()
		e.insertQueued(c.ID, r, status)
		mid := "mid-late-" + status
		e.deliver(channel.EventMessageEcho, "m:"+mid, "u-1", at(10+i), channel.MessagePayload{
			PageID: pageExt1, UserID: "u-1", MID: mid, Text: "Dạ còn ạ", IsEcho: true, EchoAppID: ownAppID,
			Metadata: r.String(), SentAt: at(10 + i),
		})
		want := int64(0)
		if status == "discarded" {
			want = 1
		}
		if n := mutMetric(e.reg.Drain(), "late_echo_after_discard", facebook.Key); n != want {
			t.Errorf("%s: late_echo_after_discard = %d, muốn %d", status, n, want)
		}
	}
	// Echo của CRM không có mid (metadata đúng client_ref) → không khớp, tin queued giữ nguyên.
	r := uuid.New()
	e.insertQueued(c.ID, r, "queued")
	e.deliver(channel.EventMessageEcho, "m:no-mid", "u-1", at(20), channel.MessagePayload{
		PageID: pageExt1, UserID: "u-1", Text: "Dạ còn ạ", IsEcho: true, EchoAppID: ownAppID, Metadata: r.String(), SentAt: at(20),
	})
	if got := e.texts(`SELECT send_status FROM conversation.messages WHERE client_ref = $1`, r); len(got) != 1 || got[0] != "queued" {
		t.Errorf("echo không mid: tin của CRM %v, muốn queued", got)
	}
}

// Tin khách chỉ có ảnh → đoạn trích "[Tệp đính kèm]", một lệnh tải, báo hiệu hội thoại đổi (hook sau tệp vẫn chạy);
// tin không nội dung, không tệp → không đoạn trích (Task 182, mutation testing M4).
func TestMutM4_ImageMessageSnippetAndSignal(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	p := message("u-1", "mid-img", "", 0)
	p.Attachments = []channel.AttachmentRef{{Type: "image", URL: "https://scontent.fbcdn.net/m.jpg"}}
	e.deliver(channel.EventMessageReceived, "m:mid-img", "u-1", at(0), p)
	e.receive("u-2", "mid-blank", "", 0)
	c := e.conversation(e.page1, "u-1")
	if deref(c.Snippet) != mutAttachmentSnippet {
		t.Errorf("đoạn trích tin ảnh = %q, muốn %q", deref(c.Snippet), mutAttachmentSnippet)
	}
	if n := e.mutFetchJobs(e.mutMessageID("mid-img")); n != 1 {
		t.Errorf("lệnh tải = %d, muốn 1", n)
	}
	if n := len(e.changedSignals(c.ID)); n != 1 {
		t.Errorf("báo hiệu sau tin ảnh = %d, muốn 1", n)
	}
	if c2 := e.conversation(e.page1, "u-2"); c2.Snippet != nil {
		t.Errorf("đoạn trích tin rỗng = %q, muốn không có", *c2.Snippet)
	}
}

// Quy tắc tự động: tin khách, comment khách qua webhook ghi một lượt chạy của quy tắc chào, tự ẩn (cổng quá hạn vẫn
// ghi lượt); cùng loại sự kiện tới bằng đường lịch sử (ViaHistory) không ghi lượt nào (Task 182, mutation testing M4).
func TestMutM4_AutomationHooksByVia(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	user := e.staff.User(t, e.tenant, "mut182@elimaz.test", "active")
	greet, err := json.Marshal(automation.GreetingConfig{Kind: automation.KindGreeting, Text: "Chào bạn"})
	if err != nil {
		t.Fatal(err)
	}
	hide, err := json.Marshal(automation.HideConfig{Kind: automation.KindAutoHide, Mode: automation.HideAll, ExceptPostIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		for kind, cfg := range map[string][]byte{automation.KindGreeting: greet, automation.KindAutoHide: hide} {
			if _, _, err := automation.SaveRule(t.Context(), tx, e.page1, kind, true, cfg, 0, user); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	runs := func(kind string) int {
		return e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.automation_runs r
			JOIN conversation.automation_rules k ON k.id = r.rule_id WHERE k.kind = $1`, kind)
	}
	e.enqueueFrom(facebook.Key, webhooks.ViaHistory, channel.EventMessageReceived, "m:h1", pageExt1, "u-h", at(0),
		message("u-h", "h1", "tin cũ", 0))
	e.enqueueFrom(facebook.Key, webhooks.ViaHistory, channel.EventCommentChanged, "c:h2", pageExt1, post1+":u-h", at(0),
		commentEv("add", post1, "h2", "", "u-h", "Lan", "comment cũ", 0))
	e.drain()
	if g, h := runs(automation.KindGreeting), runs(automation.KindAutoHide); g != 0 || h != 0 {
		t.Fatalf("sự kiện lịch sử ghi lượt: chào %d, ẩn %d; muốn 0, 0", g, h)
	}
	e.receive("u-w", "w1", "chào shop", 1)
	e.sendComment(commentEv("add", post1, "w2", "", "u-w", "Lan", "giá sao", 2))
	if g, h := runs(automation.KindGreeting), runs(automation.KindAutoHide); g != 1 || h != 1 {
		t.Fatalf("sự kiện webhook: lượt chào %d, ẩn %d; muốn 1, 1", g, h)
	}
	e.noDeadJobs()
}

// Sau khi chèn comment: comment chỉ có ảnh → một lệnh tải, đoạn trích "[Tệp đính kèm]", một báo hiệu; sửa tới trước
// add → hội thoại cập nhật (chưa đọc 1, đoạn trích bản sửa, báo hiệu); comment khách trên bài đang live → báo hiệu hội
// thoại và live.comments; comment đã xóa (xóa tới trước add) và comment của Page trên bài live không phát live.comments;
// add mang cờ đã ẩn → tin ẩn; sửa lần hai mới hơn → nội dung lần hai (Task 182, mutation testing M4).
func TestMutM4_CommentAfterInsert(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	photo := commentEv("add", post1, "c-photo", "", "u-1", "Lan", "", 0)
	photo.PhotoURL = "https://scontent.fbcdn.net/c.jpg"
	e.sendComment(photo)
	m := e.mustComment("c-photo")
	if n := e.mutFetchJobs(m.ID); n != 1 {
		t.Errorf("lệnh tải ảnh comment = %d, muốn 1", n)
	}
	if c, _ := e.commentConv(post1, "u-1"); deref(c.Snippet) != mutAttachmentSnippet || len(e.changedSignals(c.ID)) != 1 {
		t.Errorf("hội thoại comment ảnh: đoạn trích %q, báo hiệu %d; muốn %q, 1", deref(c.Snippet), len(e.changedSignals(c.ID)), mutAttachmentSnippet)
	}

	e.sendComment(commentEv("edited", post1, "c-ed", "", "u-2", "Minh", "Bản sửa", 5))
	if c, _ := e.commentConv(post1, "u-2"); c.Unread != 1 || deref(c.Snippet) != "Bản sửa" || len(e.changedSignals(c.ID)) != 1 {
		t.Errorf("sửa trước add: %+v, báo hiệu %d; muốn chưa đọc 1, đoạn trích bản sửa, 1 báo hiệu", c, len(e.changedSignals(c.ID)))
	}

	e.enqueueVia(webhooks.ViaWebhook, channel.EventLiveStatus, "l:lv:LIVE", pageExt1, channel.LiveStatusPayload{
		PageID: pageExt1, LiveVideoID: "lv-m", VideoID: post2, Status: "LIVE", Title: "Live", At: at(0),
	})
	liveSignals := func() int {
		n := 0
		for _, s := range e.bus.Signals() {
			if s.Topic == "live.comments" {
				n++
			}
		}
		return n
	}
	e.sendComment(commentEv("add", post2, "lc1", "", "u-3", "Mai", "Chốt 1", 10))
	if c, _ := e.commentConv(post2, "u-3"); len(e.changedSignals(c.ID)) != 1 || liveSignals() != 1 {
		t.Fatalf("comment live: báo hiệu hội thoại %d, live.comments %d; muốn 1, 1", len(e.changedSignals(c.ID)), liveSignals())
	}
	e.sendComment(commentEv("remove", post2, "lc2", "", "u-4", "Mai", "", 11))
	e.sendComment(commentEv("add", post2, "lc3", "lc1", pageExt1, "Shop", "Dạ chốt ạ", 12))
	if _, ok := e.commentRow("lc3"); !ok {
		t.Fatal("comment của Page trả lời comment live phải được ghi")
	}
	if n := liveSignals(); n != 1 {
		t.Errorf("live.comments sau comment đã xóa và comment của Page = %d, muốn 1", n)
	}

	hidden := commentEv("add", post1, "c-hid", "", "u-5", "Hoa", "Số em 0912", 20)
	hidden.IsHidden = true
	e.sendComment(hidden)
	if m := e.mustComment("c-hid"); !m.Hidden {
		t.Errorf("add đã ẩn: %+v, muốn ẩn", m)
	}

	e.sendComment(commentEv("add", post1, "c-2x", "", "u-6", "Lan", "bản 1", 30))
	e.sendComment(commentEv("edited", post1, "c-2x", "", "u-6", "Lan", "bản 2", 31))
	e.sendComment(commentEv("edited", post1, "c-2x", "", "u-6", "Lan", "bản 3", 32))
	if m := e.mustComment("c-2x"); deref(m.Body) != "bản 3" || m.EditedAt == nil || !m.EditedAt.Equal(at(32)) {
		t.Errorf("sửa lần hai: %+v, muốn bản 3", m)
	}
	e.noDeadJobs()
}

// Mốc đã nhận: lần đầu đánh dấu tin ra → một báo hiệu; giao cùng mốc lần nữa (không tin nào đổi) → không báo hiệu.
// Khách sửa tin hai lần (mốc tăng) → nội dung lần hai, hai dòng lịch sử (Task 182, mutation testing M4).
func TestMutM4_WatermarkSignalAndSecondEdit(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	base := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	e.receiveAt("u-1", "mid-in", "hỏi", base)
	e.echoAt("u-1", "mid-o1", "trả lời", base.Add(time.Minute))
	c := e.conversation(e.page1, "u-1")
	before := len(e.changedSignals(c.ID))
	wm := func(dedupe string) {
		e.deliver(channel.EventMessageDelivered, dedupe, "u-1", base.Add(2*time.Minute),
			channel.WatermarkPayload{PageID: pageExt1, UserID: "u-1", Watermark: base.Add(2 * time.Minute)})
	}
	wm("wm:1")
	if n := len(e.changedSignals(c.ID)) - before; n != 1 {
		t.Fatalf("đã nhận lần đầu: %d báo hiệu, muốn 1", n)
	}
	wm("wm:2")
	if n := len(e.changedSignals(c.ID)) - before; n != 1 {
		t.Fatalf("đã nhận lặp lại (không tin nào đổi): tổng %d báo hiệu, muốn 1", n)
	}

	e.receive("u-2", "mid-e", "size M", 0)
	for i, text := range []string{"size L", "size XL"} {
		e.deliver(channel.EventMessageEdited, "e:"+text, "u-2", at(10*(i+1)), channel.EditPayload{
			PageID: pageExt1, UserID: "u-2", MID: "mid-e", Text: text, EditedAt: at(10 * (i + 1)),
		})
	}
	ms := e.messages(e.conversation(e.page1, "u-2").ID)
	if len(ms) != 1 || deref(ms[0].Body) != "size XL" || ms[0].EditedAt == nil || !ms[0].EditedAt.Equal(at(20)) {
		t.Fatalf("sửa lần hai: %+v", ms)
	}
	if revs := e.texts(`SELECT body FROM conversation.message_revisions WHERE message_id = $1 ORDER BY recorded_at, id`, ms[0].ID); len(revs) != 2 || revs[1] != "size L" {
		t.Fatalf("lịch sử: %v, muốn [size M, size L]", revs)
	}
	e.noDeadJobs()
}

// live.status trạng thái chưa phát (không ánh xạ) hay thiếu mã live → không tạo phiên, việc xong; policy.enforcement
// thiếu mã Page → lỗi vĩnh viễn (Task 182, mutation testing M4).
func TestMutM4_LiveStatusAndPolicyGuards(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.enqueueVia(webhooks.ViaWebhook, channel.EventLiveStatus, "l:x:SCHED", pageExt1, channel.LiveStatusPayload{
		PageID: pageExt1, LiveVideoID: "lv-x", VideoID: post1, Status: "SCHEDULED_UNPUBLISHED", At: at(0),
	})
	e.enqueueVia(webhooks.ViaWebhook, channel.EventLiveStatus, "l:y:LIVE", pageExt1, channel.LiveStatusPayload{
		PageID: pageExt1, VideoID: post1, Status: "LIVE", At: at(0),
	})
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.live_sessions`); n != 0 {
		t.Fatalf("%d phiên live, muốn 0", n)
	}
	if n := e.mutInboundNotDone() + e.mutInboundDead(); n != 0 {
		t.Fatalf("%d việc vào chưa xong hay dead", n)
	}
	err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		return inbound.HandlePolicy(t.Context(), tx, e.deps, facebook.Key, webhooks.Inbound{
			Type: channel.EventPolicyEnforcement, Data: json.RawMessage(`{"PageID":"","Action":"block"}`), TenantID: e.tenant,
			JobID: uuid.New(), ReceivedAt: time.Now(), Via: webhooks.ViaWebhook,
		})
	})
	if !errors.Is(err, queue.ErrPermanent) {
		t.Fatalf("policy thiếu mã Page: %v, muốn ErrPermanent", err)
	}
}

// Hội thoại hộp thư mới trên kênh đọc được lịch sử → một lệnh đọc lịch sử cho hội thoại đó (sau giao xoay vòng)
// (Task 182, mutation testing M4).
func TestMutM4_NewInboxQueuesHistory(t *testing.T) {
	t.Parallel()
	e := newEnvWith(t, fakegraphFor(t))
	e.receive("u-hist", "mid-hist", "chào", 0)
	c := e.conversation(e.page1, "u-hist")
	if n := e.staff.Count(t, `SELECT count(*) FROM platform.jobs WHERE kind = $1 AND payload->>'conversationId' = $2`,
		history.FetchHistoryKind, c.ID.String()); n != 1 {
		t.Fatalf("lệnh đọc lịch sử = %d, muốn 1", n)
	}
}

// Quy tắc tự trả lời comment (không bật includeLive): comment khách mới trên bài đang live → lượt skipped_live (cờ live
// truyền đúng cho quy tắc) (Task 182, mutation testing M4).
func TestMutM4_LiveCommentSkipsAutoReply(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	user := e.staff.User(t, e.tenant, "mut182-live@elimaz.test", "active")
	cfg := `{"kind":"auto_reply_comment","replies":["Dạ shop nhắn mình ạ"],"postIds":[],"exceptPostIds":[],` +
		`"includeLive":false,"perUserPerPostHours":24}`
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), `UPDATE channel.channel_accounts SET send_mode = 'enabled' WHERE id = $1`, e.page1); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, _, err := automation.SaveRule(t.Context(), tx, e.page1, automation.KindAutoReplyComment, true, json.RawMessage(cfg), 0, user)
		return err //nolint:wrapcheck // test
	})
	now := time.Now().UTC().Truncate(time.Millisecond)
	e.enqueueVia(webhooks.ViaWebhook, channel.EventLiveStatus, "l:lv-ar:LIVE", pageExt1, channel.LiveStatusPayload{
		PageID: pageExt1, LiveVideoID: "lv-ar", VideoID: post1, Status: "LIVE", Title: "Live", At: now.Add(-time.Minute),
	})
	c := commentEv("add", post1, "lc-ar", "", "u-ar", "Mai", "Chốt 1", 0)
	c.CreatedAt = now
	e.sendComment(c)
	got := e.texts(`SELECT r.outcome FROM conversation.automation_runs r JOIN conversation.automation_rules k ON k.id = r.rule_id
		WHERE k.kind = $1`, automation.KindAutoReplyComment)
	if len(got) != 1 || got[0] != "skipped_live" {
		t.Fatalf("lượt tự trả lời comment live = %v, muốn [skipped_live]", got)
	}
}
