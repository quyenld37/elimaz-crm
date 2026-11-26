package facebook_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// fakegraphCustomerReply là comment con của khách dưới testCommentID, cùng chữ với trả lời Page sẽ gửi.
func fakegraphCustomerReply() fakegraph.Comment {
	return fakegraph.Comment{
		ID: testCommentID + "_c1", ParentID: testCommentID, FromID: testPSID, Message: "Dạ còn ạ", Created: time.Now().UTC(),
	}
}

// Lookup trả lời comment: thấy comment con của Page cùng nội dung → sent kèm mã; không thấy (kể cả nội dung khác,
// comment con của khách) → unknown, không bao giờ not_sent.
func TestLookup_CommentReply(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	after := time.Now().Add(-time.Minute)
	q := channel.LookupQuery{Purpose: channel.PurposeCommentReply, CommentID: testCommentID, Text: "Dạ còn ạ", After: after}
	r, err := e.ch.Lookup(t.Context(), e.acc, e.cred, q)
	if err != nil || r.Status != channel.OutcomeUnknown {
		t.Fatalf("chưa có trả lời: %+v, %v; muốn unknown", r, err)
	}
	e.fg.AddComment(fakegraphCustomerReply())
	if r, _ := e.ch.Lookup(t.Context(), e.acc, e.cred, q); r.Status != channel.OutcomeUnknown {
		t.Errorf("chỉ có comment con của khách cùng chữ: %+v", r)
	}
	sent := e.send(t, channel.OutboundCommand{Purpose: channel.PurposeCommentReply, CommentID: testCommentID, Text: "Dạ còn ạ"})
	r, err = e.ch.Lookup(t.Context(), e.acc, e.cred, q)
	if err != nil || r.Status != channel.OutcomeSent || r.ExternalID != sent.ExternalID {
		t.Errorf("đã trả lời: %+v, %v; muốn sent %s", r, err, sent.ExternalID)
	}
	other := q
	other.Text = "Nội dung khác"
	if r, _ := e.ch.Lookup(t.Context(), e.acc, e.cred, other); r.Status != channel.OutcomeUnknown {
		t.Errorf("nội dung khác: %+v", r)
	}
	call := e.fg.Calls(testCommentID + "/comments")[0]
	if call.Query.Get("filter") != "stream" || call.Query.Get("since") == "" {
		t.Errorf("query tra = %v; muốn filter=stream, since", call.Query)
	}
}

// Lookup ẩn/hiện: is_hidden đúng như muốn → sent; khác → not_sent. Tin, nhắn riêng → unknown không gọi Meta.
func TestLookup_HideAndMessages(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	ctx := t.Context()
	hide := channel.LookupQuery{Purpose: channel.PurposeHide, CommentID: testCommentID, WantHidden: new(true)}
	if r, err := e.ch.Lookup(ctx, e.acc, e.cred, hide); err != nil || r.Status != channel.OutcomeNotSent {
		t.Errorf("muốn ẩn, đang hiện: %+v, %v; muốn not_sent", r, err)
	}
	e.send(t, channel.OutboundCommand{Purpose: channel.PurposeHide, CommentID: testCommentID, Hidden: new(true)})
	if r, err := e.ch.Lookup(ctx, e.acc, e.cred, hide); err != nil || r.Status != channel.OutcomeSent {
		t.Errorf("muốn ẩn, đã ẩn: %+v, %v; muốn sent", r, err)
	}
	if r, err := e.ch.Lookup(ctx, e.acc, e.cred, channel.LookupQuery{Purpose: channel.PurposeHide, CommentID: testCommentID}); err != nil ||
		r.Status != channel.OutcomeUnknown {
		t.Errorf("không nói muốn gì: %+v, %v; muốn unknown", r, err)
	}
	before := len(e.fg.Calls(""))
	for _, p := range []channel.Purpose{channel.PurposeMessage, channel.PurposePrivateReply} {
		if r, err := e.ch.Lookup(ctx, e.acc, e.cred, channel.LookupQuery{Purpose: p, Text: "chào"}); err != nil || r.Status != channel.OutcomeUnknown {
			t.Errorf("%s: %+v, %v; muốn unknown", p, r, err)
		}
	}
	if n := len(e.fg.Calls("")) - before; n != 0 {
		t.Errorf("tra tin gọi Meta %d lần, muốn 0", n)
	}
	e.fg.RevokeToken(testToken)
	r, err := e.ch.Lookup(ctx, e.acc, e.cred, hide)
	if r.Status != channel.OutcomeUnknown || e.ch.ClassifyError(err) != channel.ErrorAuth {
		t.Errorf("token thu hồi: %+v, %v; muốn unknown kèm lỗi auth", r, err)
	}
}
