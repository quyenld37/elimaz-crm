package facebook_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

const (
	testCommentID = "900000000000001_800000000000001"
	testPostID    = testPageID + "_900000000000001"
	messagesPath  = testPageID + "/messages"
)

// outEnv là máy Meta giả có một Page, một bài, một comment của khách và adapter đầy đủ trỏ vào nó.
type outEnv struct {
	fg   *fakegraph.Server
	hc   *http.Client
	ch   *facebook.Channel
	acc  channel.ChannelAccount
	cred channel.Credentials
}

func newOutEnv(t *testing.T) outEnv {
	t.Helper()
	fg := fakegraph.New(t, fakegraph.Options{AppID: testAppID, AppSecret: testAppSecret})
	fg.AddPage(fakegraph.Page{ID: testPageID, Name: "Shop thử", Token: testToken})
	fg.AddProfile(fakegraph.Profile{PSID: testPSID, FirstName: "Lan", LastName: "Nguyễn", ProfilePic: "https://scontent.fbcdn.net/p.jpg"})
	fg.AddPost(fakegraph.Post{
		ID: testPostID, PageID: testPageID, Message: "Áo thun mới về", PermalinkURL: "https://www.facebook.com/" + testPostID,
		Created: time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC),
	})
	fg.AddComment(fakegraph.Comment{
		ID: testCommentID, ParentID: testPostID, FromID: testPSID, FromName: "Lan", Message: "Chốt 1 áo",
		Created: time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC),
	})
	hc := fg.Client()
	ch := facebook.New(facebook.Options{
		BaseURL: fg.URL(), StreamingBaseURL: fg.URL(), Version: "v26.0", AppID: testAppID,
		AppSecret: logging.Secret(testAppSecret), VerifyToken: logging.Secret(testVerifyToken), HTTP: hc,
		AttachmentHosts: []string{"fbcdn.net"},
	})
	acc := channel.ChannelAccount{
		ID: uuid.New(), Channel: facebook.Key, ExternalID: testPageID, Name: "Shop thử", Status: "active", SendMode: "enabled",
	}
	return outEnv{fg: fg, hc: hc, ch: ch, acc: acc, cred: channel.Credentials{AccessToken: testToken}}
}

// refuseNext làm lời gọi kế tiếp chắc chắn chưa gửi: máy giả đóng kết nối mới trước bắt tay TLS, và client bỏ kết nối
// đang giữ (không thì nó có thể ghi yêu cầu lên kết nối cũ máy giả vừa đóng — ca "có thể đã gửi").
func (e outEnv) refuseNext() {
	e.fg.RefuseNextConn()
	e.hc.CloseIdleConnections()
}

func (e outEnv) send(t *testing.T, cmd channel.OutboundCommand) channel.SendOutcome {
	t.Helper()
	out, err := e.ch.Send(t.Context(), e.acc, e.cred, cmd)
	if err != nil && facebook.Classify(err) != channel.ErrorAuth {
		t.Fatalf("Send: %v", err)
	}
	return out
}

// sentBody là thân JSON của lần gửi thứ i tới {page-id}/messages mà máy giả nhận.
func (e outEnv) sentBody(t *testing.T, i int) map[string]any {
	t.Helper()
	calls := e.fg.Calls(messagesPath)
	if len(calls) <= i {
		t.Fatalf("máy giả nhận %d lần gửi, muốn lần %d", len(calls), i)
	}
	var m map[string]any
	if err := json.Unmarshal(calls[i].Body, &m); err != nil {
		t.Fatalf("thân gửi: %v", err)
	}
	return m
}

func textCmd(text string) channel.OutboundCommand {
	return channel.OutboundCommand{
		Purpose: channel.PurposeMessage, RecipientID: testPSID, Text: text, Mode: channel.SendStandard, ClientRef: "ref-1",
	}
}

func field(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

// Tin chữ: metadata = ClientRef, RESPONSE, không tag; Human Agent: MESSAGE_TAG + HUMAN_AGENT; ảnh: chỉ attachment,
// không text, is_reusable false; nút trả lời nhanh gửi đúng.
func TestSend_MessageBody(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	out := e.send(t, textCmd("chào chị"))
	if out.Status != channel.OutcomeSent || !strings.HasPrefix(out.ExternalID, "m_") || out.RecipientID != testPSID {
		t.Fatalf("tin chữ: %+v", out)
	}
	b := e.sentBody(t, 0)
	if field(b, "message", "metadata") != "ref-1" || field(b, "messaging_type") != "RESPONSE" || field(b, "tag") != nil ||
		field(b, "message", "text") != "chào chị" || field(b, "recipient", "id") != testPSID {
		t.Errorf("thân tin chữ = %v", b)
	}

	hc := textCmd("em gửi lại chị")
	hc.Mode = channel.SendHumanAgent
	if out := e.send(t, hc); out.Status != channel.OutcomeSent {
		t.Fatalf("human agent: %+v", out)
	}
	b = e.sentBody(t, 1)
	if field(b, "messaging_type") != "MESSAGE_TAG" || field(b, "tag") != "HUMAN_AGENT" {
		t.Errorf("thân human agent = %v", b)
	}

	img := channel.OutboundCommand{
		Purpose: channel.PurposeMessage, RecipientID: testPSID, ImageURL: "https://files.example/a.jpg", Mode: channel.SendStandard,
		ClientRef: "ref-3", QuickReplies: []channel.QuickReply{{Title: "Size M", Payload: "SIZE_M"}},
	}
	if out := e.send(t, img); out.Status != channel.OutcomeSent {
		t.Fatalf("ảnh: %+v", out)
	}
	b = e.sentBody(t, 2)
	if field(b, "message", "text") != nil || field(b, "message", "attachment", "type") != "image" ||
		field(b, "message", "attachment", "payload", "url") != "https://files.example/a.jpg" ||
		field(b, "message", "attachment", "payload", "is_reusable") != false {
		t.Errorf("thân ảnh = %v", b)
	}
	qr, _ := field(b, "message", "quick_replies").([]any)
	if len(qr) != 1 || field(qr[0].(map[string]any), "title") != "Size M" || field(qr[0].(map[string]any), "payload") != "SIZE_M" ||
		field(qr[0].(map[string]any), "content_type") != "text" {
		t.Errorf("nút trả lời nhanh = %v", qr)
	}
}

// Kiểm trước khi gọi Meta: 14 nút, tiêu đề 21 ký tự, cả chữ lẫn ảnh, không có gì, quá 2 000 ký tự, thiếu người nhận,
// cách gửi lạ, không token → rejected, máy giả không nhận lời gọi nào.
func TestSend_RejectBeforeCall(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	qrs := func(n int, title string) []channel.QuickReply {
		out := make([]channel.QuickReply, n)
		for i := range out {
			out[i] = channel.QuickReply{Title: title, Payload: "P"}
		}
		return out
	}
	with := func(f func(*channel.OutboundCommand)) channel.OutboundCommand {
		c := textCmd("chào")
		f(&c)
		return c
	}
	for name, cmd := range map[string]channel.OutboundCommand{
		"14 nút":              with(func(c *channel.OutboundCommand) { c.QuickReplies = qrs(14, "Có") }),
		"tiêu đề 21 ký tự":    with(func(c *channel.OutboundCommand) { c.QuickReplies = qrs(1, strings.Repeat("á", 21)) }),
		"tiêu đề rỗng":        with(func(c *channel.OutboundCommand) { c.QuickReplies = qrs(1, "") }),
		"chữ và ảnh":          with(func(c *channel.OutboundCommand) { c.ImageURL = "https://files.example/a.jpg" }),
		"không chữ không ảnh": with(func(c *channel.OutboundCommand) { c.Text = "" }),
		"quá 2000 ký tự":      with(func(c *channel.OutboundCommand) { c.Text = strings.Repeat("ạ", 2001) }),
		"thiếu người nhận":    with(func(c *channel.OutboundCommand) { c.RecipientID = "" }),
		"cách gửi lạ":         with(func(c *channel.OutboundCommand) { c.Mode = channel.SendPrivateReply }),
		"nhắn riêng thiếu comment": with(func(c *channel.OutboundCommand) {
			c.Purpose, c.Mode = channel.PurposePrivateReply, channel.SendPrivateReply
		}),
		"trả lời comment không chữ": {Purpose: channel.PurposeCommentReply, CommentID: testCommentID},
		"ẩn không nói ẩn hay hiện":  {Purpose: channel.PurposeHide, CommentID: testCommentID},
		"mục đích lạ":               with(func(c *channel.OutboundCommand) { c.Purpose = "broadcast" }),
	} {
		out := e.send(t, cmd)
		if out.Status != channel.OutcomeRejected || out.Detail == "" {
			t.Errorf("%s: %+v; muốn rejected có Detail", name, out)
		}
	}
	if n := len(e.fg.Calls("")); n != 0 {
		t.Errorf("máy giả nhận %d lời gọi, muốn 0", n)
	}
	out, err := e.ch.Send(t.Context(), e.acc, channel.Credentials{}, textCmd("chào"))
	if err != nil || out.Status != channel.OutcomeRejected || out.Reason != channel.DenyNoPermission {
		t.Errorf("không token: %+v, %v", out, err)
	}
	max13 := with(func(c *channel.OutboundCommand) { c.QuickReplies = qrs(13, strings.Repeat("á", 20)) })
	if out := e.send(t, max13); out.Status != channel.OutcomeSent {
		t.Errorf("13 nút, tiêu đề 20 ký tự: %+v", out)
	}
}

// Nhắn riêng: recipient.comment_id, Meta chỉ cho một lần (10900 → private_reply_used). Trả lời comment:
// POST {comment-id}/comments. Ẩn/hiện: POST {comment-id} is_hidden.
func TestSend_CommentPurposes(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	pr := channel.OutboundCommand{
		Purpose: channel.PurposePrivateReply, CommentID: testCommentID, Text: "Shop inbox chị nhé", Mode: channel.SendPrivateReply,
		ClientRef: "ref-pr",
	}
	if out := e.send(t, pr); out.Status != channel.OutcomeSent || out.ExternalID == "" {
		t.Fatalf("nhắn riêng: %+v", out)
	}
	if b := e.sentBody(t, 0); field(b, "recipient", "comment_id") != testCommentID || field(b, "recipient", "id") != nil ||
		field(b, "message", "metadata") != "ref-pr" {
		t.Errorf("thân nhắn riêng = %v", b)
	}
	if out := e.send(t, pr); out.Status != channel.OutcomeRejected || out.Reason != channel.DenyPrivateReplyUsed || out.Code != 10900 {
		t.Errorf("nhắn riêng lần hai: %+v", out)
	}

	reply := e.send(t, channel.OutboundCommand{Purpose: channel.PurposeCommentReply, CommentID: testCommentID, Text: "Dạ còn ạ"})
	if reply.Status != channel.OutcomeSent || reply.ExternalID == "" {
		t.Fatalf("trả lời comment: %+v", reply)
	}
	if c, ok := e.fg.CommentByID(reply.ExternalID); !ok || c.Message != "Dạ còn ạ" || c.ParentID != testCommentID || c.FromID != testPageID {
		t.Errorf("comment trả lời trên máy giả = %+v, %v", c, ok)
	}

	for _, hide := range []bool{true, false} {
		out := e.send(t, channel.OutboundCommand{Purpose: channel.PurposeHide, CommentID: testCommentID, Hidden: new(hide)})
		if out.Status != channel.OutcomeSent || out.ExternalID != testCommentID {
			t.Fatalf("ẩn=%v: %+v", hide, out)
		}
		if c, _ := e.fg.CommentByID(testCommentID); c.Hidden != hide {
			t.Errorf("ẩn=%v: comment trên máy giả Hidden = %v", hide, c.Hidden)
		}
	}
}

// Kết quả theo lớp lỗi: 551 → rejected blocked_by_user; 10/2018278 → window_closed; 613 → not_sent, RetryAfter ≥ 60 s;
// đóng kết nối trước khi gửi → not_sent; DropAfterRead → unknown; 502 HTML → unknown; 100 → rejected không Reason;
// 190 → rejected no_permission kèm lỗi auth.
func TestSend_Outcomes(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	for _, c := range []struct {
		name   string
		fault  func()
		status channel.OutcomeStatus
		reason channel.DenyReason
		code   int
	}{
		{"551", func() { e.fg.FailNext(messagesPath, fakegraph.GraphErrorSpec{Code: 551}) }, channel.OutcomeRejected, channel.DenyBlockedByUser, 551},
		{
			"10/2018278", func() { e.fg.FailNext(messagesPath, fakegraph.GraphErrorSpec{Code: 10, Subcode: 2018278}) },
			channel.OutcomeRejected, channel.DenyWindowClosed, 10,
		},
		{"613", func() { e.fg.FailNext(messagesPath, fakegraph.GraphErrorSpec{Code: 613}) }, channel.OutcomeNotSent, "", 613},
		{"chưa gửi", e.refuseNext, channel.OutcomeNotSent, "", 0},
		{"rớt sau khi đọc", func() { e.fg.DropAfterRead(messagesPath) }, channel.OutcomeUnknown, "", 0},
		{
			"502 HTML", func() {
				e.fg.FailNext(messagesPath, fakegraph.GraphErrorSpec{HTTPStatus: 502, RawBody: "<html>Bad Gateway</html>"})
			}, channel.OutcomeUnknown, "", 0,
		},
		{"100", func() { e.fg.FailNext(messagesPath, fakegraph.GraphErrorSpec{Code: 100}) }, channel.OutcomeRejected, "", 100},
	} {
		c.fault()
		out := e.send(t, textCmd("chào"))
		if out.Status != c.status || out.Reason != c.reason || out.Code != c.code {
			t.Errorf("%s: %+v; muốn %s/%q/%d", c.name, out, c.status, c.reason, c.code)
		}
		if out.Status != channel.OutcomeSent && out.Detail == "" {
			t.Errorf("%s: thiếu Detail", c.name)
		}
		if c.name == "613" && out.RetryAfter < time.Minute {
			t.Errorf("613: RetryAfter = %v, muốn ≥ 60 s", out.RetryAfter)
		}
	}
	e.fg.RevokeToken(testToken)
	out, err := e.ch.Send(t.Context(), e.acc, e.cred, textCmd("chào"))
	if out.Status != channel.OutcomeRejected || out.Reason != channel.DenyNoPermission || out.Code != 190 ||
		e.ch.ClassifyError(err) != channel.ErrorAuth {
		t.Errorf("token thu hồi: %+v, %v; muốn rejected no_permission, lỗi auth", out, err)
	}
	if strings.Contains(out.Detail, testToken) {
		t.Error("Detail chứa token")
	}
}
