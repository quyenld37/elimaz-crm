package facebook_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// liveVideoID là mã phiên live thử.
const liveVideoID = "lv-171"

// liveBase là giờ comment live đầu tiên của các test.
var liveBase = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // hằng thời gian của test

// liveComment là comment live thứ i (cách nhau 1 giây từ liveBase) của khách u<i>.
func liveComment(i int) fakegraph.Comment {
	return fakegraph.Comment{
		ID: fmt.Sprintf("%s_c%04d", liveVideoID, i), FromID: "u" + strconv.Itoa(i), FromName: "Khách " + strconv.Itoa(i),
		Message: "chốt " + strconv.Itoa(i), Created: liveBase.Add(time.Duration(i) * time.Second),
	}
}

// commentOf giải payload comment của một sự kiện.
func commentOf(t *testing.T, e webhooks.InboundEvent) channel.CommentPayload {
	t.Helper()
	var p channel.CommentPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// PollComments (02 mục 2, dự phòng của luồng SSE): GET {live-video-id}/comments?filter=stream&order=chronological&
// since=<unix>&fields=…,parent{id}&limit=100, đi hết các trang theo con trỏ after; mỗi comment một comment.changed (verb
// add) với khóa chống trùng như webhook, bài là mã live; trả lời comment có ParentID là comment cha. Token hỏng → lỗi
// lớp auth.
func TestPollAndCountComments(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	e.fg.AddLiveVideo(fakegraph.LiveVideo{ID: liveVideoID, PageID: testPageID, Status: "LIVE"})
	for i := range 250 {
		c := liveComment(i)
		if i == 7 {
			c.ParentID = liveComment(3).ID // trả lời comment 3
		}
		e.fg.AddLiveComment(liveVideoID, c)
	}
	since := liveBase.Add(9 * time.Second)
	evs, err := e.ch.PollComments(t.Context(), e.acc, e.cred, liveVideoID, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 240 { // comment 10…249 (Meta lọc created_time > since theo giây)
		t.Fatalf("hỏi định kỳ từ comment 9: %d sự kiện, muốn 240", len(evs))
	}
	first := commentOf(t, evs[0])
	want := channel.CommentPayload{
		PageID: testPageID, PostID: liveVideoID, CommentID: liveComment(10).ID, ParentID: liveVideoID, FromID: "u10",
		FromName: "Khách 10", Text: "chốt 10", Verb: "add", CreatedAt: liveBase.Add(10 * time.Second),
	}
	if first != want || evs[0].Type != channel.EventCommentChanged || !evs[0].OccurredAt.Equal(want.CreatedAt) ||
		evs[0].Route != (webhooks.Route{Kind: "fb_page", ExternalKey: testPageID}) {
		t.Errorf("sự kiện đầu: %+v %+v", evs[0], first)
	}
	if last := commentOf(t, evs[239]); last.CommentID != liveComment(249).ID {
		t.Errorf("sự kiện cuối: %+v (phải theo thứ tự thời gian)", last)
	}
	calls := e.fg.Calls(liveVideoID + "/comments")
	if len(calls) != 3 {
		t.Fatalf("%d trang, muốn 3 (limit 100)", len(calls))
	}
	q := calls[0].Query
	if q.Get("filter") != "stream" || q.Get("order") != "chronological" || q.Get("limit") != "100" ||
		q.Get("since") != strconv.FormatInt(since.Unix(), 10) ||
		q.Get("fields") != "id,from{id,name},message,created_time,parent{id}" || !calls[0].ProofOK || !calls[0].Versioned {
		t.Errorf("yêu cầu trang đầu: %v", q)
	}
	if calls[1].Query.Get("after") != "100" || calls[2].Query.Get("after") != "200" {
		t.Errorf("con trỏ trang: %q, %q", calls[1].Query.Get("after"), calls[2].Query.Get("after"))
	}
	all, err := e.ch.PollComments(t.Context(), e.acc, e.cred, liveVideoID, time.Time{})
	if err != nil || len(all) != 250 {
		t.Fatalf("hỏi từ đầu phiên: %d, %v", len(all), err)
	}
	if p := commentOf(t, all[7]); p.ParentID != liveComment(3).ID {
		t.Errorf("trả lời comment: ParentID = %q", p.ParentID)
	}
	if q := e.fg.Calls(liveVideoID + "/comments")[3].Query; q.Has("since") {
		t.Errorf("since rỗng vẫn gửi since: %v", q)
	}
	bad := channel.Credentials{AccessToken: "sai"}
	if _, err := e.ch.PollComments(t.Context(), e.acc, bad, liveVideoID, since); e.ch.ClassifyError(err) != channel.ErrorAuth {
		t.Errorf("PollComments token hỏng: %v", err)
	}
}

// CountComments đọc summary.total_count (filter=stream, limit=0); Meta báo số khác số liệt kê được → trả số Meta báo;
// token hỏng → lỗi lớp auth.
func TestCountComments(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	e.fg.AddLiveVideo(fakegraph.LiveVideo{ID: liveVideoID, PageID: testPageID, Status: "LIVE"})
	for i := range 250 {
		e.fg.AddLiveComment(liveVideoID, liveComment(i))
	}
	n, err := e.ch.CountComments(t.Context(), e.acc, e.cred, liveVideoID)
	if err != nil || n != 250 {
		t.Errorf("CountComments = %d, %v; muốn 250", n, err)
	}
	cs := e.fg.Calls(liveVideoID + "/comments")
	if q := cs[len(cs)-1].Query; q.Get("summary") != "total_count" || q.Get("filter") != "stream" || q.Get("limit") != "0" {
		t.Errorf("yêu cầu đếm: %v", q)
	}
	e.fg.SetLiveCommentTotal(liveVideoID, 605)
	if n, _ := e.ch.CountComments(t.Context(), e.acc, e.cred, liveVideoID); n != 605 {
		t.Errorf("total_count Meta báo 605: %d", n)
	}
	bad := channel.Credentials{AccessToken: "sai"}
	if _, err := e.ch.CountComments(t.Context(), e.acc, bad, liveVideoID); e.ch.ClassifyError(err) != channel.ErrorAuth {
		t.Errorf("CountComments token hỏng: %v", err)
	}
}

// Comment không mã hay không giờ bị bỏ; comment của chính Page có FromPage.
func TestPollComments_SkipsInvalidAndMarksPage(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	e.fg.AddLiveVideo(fakegraph.LiveVideo{ID: liveVideoID, PageID: testPageID, Status: "LIVE"})
	e.fg.AddLiveComment(liveVideoID, fakegraph.Comment{ID: "", FromID: "u1", Message: "x", Created: liveBase})
	e.fg.AddLiveComment(liveVideoID, fakegraph.Comment{ID: "pc1", FromID: testPageID, Message: "Shop chào cả nhà", Created: liveBase})
	evs, err := e.ch.PollComments(t.Context(), e.acc, e.cred, liveVideoID, time.Time{})
	if err != nil || len(evs) != 1 {
		t.Fatalf("%d sự kiện, %v; muốn 1", len(evs), err)
	}
	if p := commentOf(t, evs[0]); !p.FromPage || p.CommentID != "pc1" {
		t.Errorf("comment của Page: %+v", p)
	}
}
