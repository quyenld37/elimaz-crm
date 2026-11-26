package facebook_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// nextWithin đọc một lô của luồng st, tối đa d.
func nextWithin(t *testing.T, st channel.CommentStream, d time.Duration) ([]webhooks.InboundEvent, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), d)
	defer cancel()
	return st.Next(ctx) //nolint:wrapcheck // test
}

// StreamComments (02 mục 2): GET <streaming>/{live-video-id}/live_comments (không tiền tố phiên bản) có comment_rate,
// fields, appsecret_proof, token ở header (không nằm trong URL); mỗi "data:" là một comment → comment.changed verb add,
// bài là mã live, khóa chống trùng đúng như webhook feed của cùng comment (ba đường đổ về một chỗ); Next trả lô gồm mọi
// comment đã tới; ctx của Next hết hạn không đóng luồng; DropStream → Next trả lỗi; Close đóng kết nối.
func TestStreamComments(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	e.fg.AddLiveVideo(fakegraph.LiveVideo{ID: liveVideoID, PageID: testPageID, VideoID: "v-171", Status: "LIVE"})
	st, err := e.ch.StreamComments(t.Context(), e.acc, e.cred, liveVideoID, liveBase)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	calls := e.fg.Calls(liveVideoID + "/live_comments")
	if len(calls) != 1 {
		t.Fatalf("yêu cầu luồng: %d", len(calls))
	}
	c := calls[0]
	if c.Versioned || c.Query.Get("comment_rate") != "ten_per_second" || !c.ProofOK ||
		c.Query.Get("fields") != "id,from{id,name},message,created_time" || c.Token != testToken || c.Query.Has("access_token") {
		t.Errorf("yêu cầu luồng: versioned %v, query %v, token %q", c.Versioned, c.Query, c.Token)
	}
	if _, err := nextWithin(t, st, 100*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("chưa có comment: %v; muốn hết hạn ctx", err)
	}
	for i := range 3 {
		e.fg.PushLiveComment(liveVideoID, liveComment(i))
	}
	var got []webhooks.InboundEvent
	for len(got) < 3 {
		b, err := nextWithin(t, st, 5*time.Second)
		if err != nil {
			t.Fatalf("đọc luồng: %v (đã có %d)", err, len(got))
		}
		got = append(got, b...)
	}
	if p := commentOf(t, got[0]); p != (channel.CommentPayload{
		PageID: testPageID, PostID: liveVideoID, CommentID: liveComment(0).ID, ParentID: liveVideoID, FromID: "u0",
		FromName: "Khách 0", Text: "chốt 0", Verb: "add", CreatedAt: liveBase,
	}) {
		t.Errorf("comment đầu: %+v", p)
	}
	// Cùng comment qua webhook feed: cùng khóa chống trùng.
	raw := fmt.Sprintf(`{"object":"page","entry":[{"id":%q,"time":1,"changes":[{"field":"feed","value":{"from":{"id":"u0","name":"Khách 0"},"post_id":%q,"comment_id":%q,"created_time":%d,"item":"comment","verb":"add","message":"chốt 0"}}]}]}`,
		testPageID, liveVideoID, liveComment(0).ID, liveBase.Unix())
	res, err := e.ch.Split([]byte(raw))
	if err != nil || len(res.Events) != 1 {
		t.Fatalf("tách webhook: %+v, %v", res, err)
	}
	if res.Events[0].DedupeKey != got[0].DedupeKey || res.Events[0].OrderKey != got[0].OrderKey {
		t.Errorf("khóa luồng %q/%q, khóa webhook %q/%q", got[0].DedupeKey, got[0].OrderKey, res.Events[0].DedupeKey, res.Events[0].OrderKey)
	}
	e.fg.DropStream(liveVideoID)
	if _, err := nextWithin(t, st, 5*time.Second); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Meta cắt luồng: %v; muốn lỗi luồng", err)
	}
	st2, err := e.ch.StreamComments(t.Context(), e.acc, e.cred, liveVideoID, liveBase)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return e.fg.StreamCount(liveVideoID) == 1 })
	if err := st2.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return e.fg.StreamCount(liveVideoID) == 0 })
	if _, err := st2.Next(t.Context()); err == nil {
		t.Error("Next sau Close phải lỗi")
	}
}

// Mở luồng lỗi: 404 của Meta → *GraphError mã 404; token hỏng → lớp auth; trả lời không phải text/event-stream → lỗi.
func TestStreamComments_OpenErrors(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	path := liveVideoID + "/live_comments"
	e.fg.FailNext(path, fakegraph.GraphErrorSpec{HTTPStatus: http.StatusNotFound, Code: 803, Type: "OAuthException", Message: "(#803) not found"})
	_, err := e.ch.StreamComments(t.Context(), e.acc, e.cred, liveVideoID, liveBase)
	if ge, ok := errors.AsType[*facebook.GraphError](err); !ok || ge.HTTPStatus != http.StatusNotFound {
		t.Errorf("404: %v", err)
	}
	_, err = e.ch.StreamComments(t.Context(), e.acc, channel.Credentials{AccessToken: "sai"}, liveVideoID, liveBase)
	if e.ch.ClassifyError(err) != channel.ErrorAuth {
		t.Errorf("token hỏng: %v", err)
	}
	if strings.Contains(fmt.Sprint(err), "sai") {
		t.Errorf("lỗi chứa token: %v", err)
	}
	e.fg.FailNext(path, fakegraph.GraphErrorSpec{HTTPStatus: http.StatusOK, RawBody: "<html>ok</html>"})
	if _, err := e.ch.StreamComments(t.Context(), e.acc, e.cred, liveVideoID, liveBase); err == nil {
		t.Error("trả lời không phải text/event-stream phải lỗi")
	}
}

// Bộ đọc text/event-stream: nhiều dòng data nối bằng "\n", dòng chú thích và trường khác bỏ qua, CRLF, sự kiện cuối
// không có dòng trống bị bỏ (đúng chuẩn), dòng dài quá 64 KiB → lỗi.
func TestReadSSE(t *testing.T) {
	t.Parallel()
	in := ": heartbeat\r\nevent: comment\r\ndata: {\"a\":\r\ndata: 1}\r\nid: 7\r\n\r\ndata:x\n\ndata: y\n\n\n\ndata: cut"
	var got []string
	err := facebook.ReadSSE(strings.NewReader(in), func(d []byte) bool { got = append(got, string(d)); return true })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "{\"a\":\n1}|x|y" {
		t.Errorf("sự kiện: %q", got)
	}
	long := "data: " + strings.Repeat("a", 70<<10) + "\n\n"
	if err := facebook.ReadSSE(strings.NewReader(long), func([]byte) bool { return true }); err == nil {
		t.Error("dòng quá 64 KiB phải lỗi")
	}
	n := 0
	_ = facebook.ReadSSE(strings.NewReader("data: 1\n\ndata: 2\n\n"), func([]byte) bool { n++; return false })
	if n != 1 {
		t.Errorf("emit trả false phải dừng: %d", n)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("quá 5 s chưa đúng")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
