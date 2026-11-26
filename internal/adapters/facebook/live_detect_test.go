package facebook_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// DetectLive (Q5, LV01): chỉ trả phiên LIVE của Page, có mã live, mã video, tiêu đề; yêu cầu lọc broadcast_status và
// đúng trường. LiveStatus đọc trạng thái hiện tại (LIVE_STOPPED sau khi dừng). Token hỏng → lỗi lớp auth.
func TestDetectLiveAndStatus(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	e.fg.AddLiveVideo(fakegraph.LiveVideo{ID: "lv1", PageID: testPageID, VideoID: "v1", Status: "LIVE", Title: "Live ca trưa"})
	e.fg.AddLiveVideo(fakegraph.LiveVideo{ID: "lv2", PageID: testPageID, VideoID: "v2", Status: "VOD"})
	e.fg.AddLiveVideo(fakegraph.LiveVideo{ID: "lv3", PageID: "999", VideoID: "v3", Status: "LIVE"})
	vids, err := e.ch.DetectLive(t.Context(), e.acc, e.cred)
	if err != nil {
		t.Fatal(err)
	}
	if len(vids) != 1 || vids[0] != (channel.LiveVideo{LiveVideoID: "lv1", VideoID: "v1", Status: "LIVE", Title: "Live ca trưa"}) {
		t.Errorf("phiên đang live: %+v", vids)
	}
	calls := e.fg.Calls(testPageID + "/live_videos")
	if len(calls) != 1 || calls[0].Query.Get("broadcast_status") != `["LIVE"]` ||
		calls[0].Query.Get("fields") != "id,status,video{id},permalink_url,title,creation_time" {
		t.Errorf("yêu cầu live_videos: %+v", calls)
	}
	e.fg.SetLiveStatus("lv1", "LIVE_STOPPED")
	if st, err := e.ch.LiveStatus(t.Context(), e.acc, e.cred, "lv1"); err != nil || st != "LIVE_STOPPED" {
		t.Errorf("trạng thái lv1 = %q, %v", st, err)
	}
	if vids, err := e.ch.DetectLive(t.Context(), e.acc, e.cred); err != nil || len(vids) != 0 {
		t.Errorf("sau khi dừng: %+v, %v", vids, err)
	}
	if _, err := e.ch.DetectLive(t.Context(), e.acc, channel.Credentials{AccessToken: "sai"}); e.ch.ClassifyError(err) != channel.ErrorAuth {
		t.Errorf("token hỏng: %v", err)
	}
}
