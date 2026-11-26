package facebook

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// liveDetectFields là các trường đọc của một phiên live khi nhận biết (Q5).
const liveDetectFields = "id,status,video{id},permalink_url,title,creation_time"

type graphLiveVideo struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Video  *struct {
		ID string `json:"id"`
	} `json:"video"`
	CreationTime flexTime `json:"creation_time"`
}

// DetectLive liệt kê phiên đang phát của Page (GET {page-id}/live_videos?broadcast_status=["LIVE"]); chỉ trả phiên có
// trạng thái LIVE (lọc lại phía CRM). Mã bài của video live chưa xác minh (E4) nên PostID để trống.
func (c *Channel) DetectLive(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials) ([]channel.LiveVideo, error) {
	var out struct {
		Data []graphLiveVideo `json:"data"`
	}
	q := url.Values{"broadcast_status": {`["LIVE"]`}, "fields": {liveDetectFields}}
	if _, err := c.client.Do(ctx, http.MethodGet, acc.ExternalID+"/live_videos", cred.AccessToken, q, nil, &out); err != nil {
		return nil, err
	}
	vids := make([]channel.LiveVideo, 0, len(out.Data))
	for _, v := range out.Data {
		if v.ID == "" || !strings.EqualFold(v.Status, "LIVE") {
			continue
		}
		lv := channel.LiveVideo{LiveVideoID: v.ID, Status: strings.ToUpper(v.Status), Title: v.Title, StartedAt: v.CreationTime.t}
		if v.Video != nil {
			lv.VideoID = v.Video.ID
		}
		vids = append(vids, lv)
	}
	return vids, nil
}

// LiveStatus đọc trạng thái hiện tại của phiên live videoID (GET {live-video-id}?fields=status): LIVE, LIVE_STOPPED,
// VOD…
func (c *Channel) LiveStatus(ctx context.Context, _ channel.ChannelAccount, cred channel.Credentials, videoID string) (string, error) {
	var out struct {
		Status string `json:"status"`
	}
	if _, err := c.client.Do(ctx, http.MethodGet, videoID, cred.AccessToken, url.Values{"fields": {"status"}}, nil, &out); err != nil {
		return "", err
	}
	return strings.ToUpper(out.Status), nil
}
