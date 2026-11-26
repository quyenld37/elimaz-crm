package facebook

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

var _ channel.LiveSource = (*Channel)(nil)

const (
	// liveCommentFields là các trường của một comment live (luồng SSE); hỏi định kỳ thêm parent{id}.
	liveCommentFields = "id,from{id,name},message,created_time"
	livePollFields    = liveCommentFields + ",parent{id}"
	// livePollLimit là số comment mỗi trang khi hỏi định kỳ; livePollMaxPages chặn vòng trang (100 nghìn comment).
	livePollLimit    = 100
	livePollMaxPages = 1000
)

// liveCommentsPage là một trang {live-video-id}/comments.
type liveCommentsPage struct {
	Data   []graphComment `json:"data"`
	Paging struct {
		Cursors struct {
			After string `json:"after"`
		} `json:"cursors"`
		Next string `json:"next"`
	} `json:"paging"`
}

// PollComments đọc comment của phiên live videoID sau since (zero: từ đầu) theo thứ tự thời gian, đi hết các trang
// (GET {live-video-id}/comments?filter=stream&order=chronological&since=<unix>&limit=100), mỗi comment một
// comment.changed verb add (liveCommentEvent: cùng khóa chống trùng như webhook feed). Bên gọi ghi bằng
// webhooks.EnqueueInbound với via = live_poll. Lỗi Graph ở trang nào → trả lỗi, bỏ phần đã đọc.
func (c *Channel) PollComments(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials, videoID string,
	since time.Time,
) ([]webhooks.InboundEvent, error) {
	q := url.Values{
		"filter": {"stream"}, "order": {"chronological"}, "fields": {livePollFields},
		"limit": {strconv.Itoa(livePollLimit)},
	}
	if !since.IsZero() {
		q.Set("since", strconv.FormatInt(since.Unix(), 10))
	}
	s := &splitter{res: webhooks.SplitResult{Skipped: map[string]int{}}}
	for range livePollMaxPages {
		var out liveCommentsPage
		if _, err := c.client.Do(ctx, http.MethodGet, videoID+"/comments", cred.AccessToken, q, nil, &out); err != nil {
			return nil, err
		}
		for _, cm := range out.Data {
			s.liveComment(acc.ExternalID, videoID, cm)
		}
		after := out.Paging.Cursors.After
		if out.Paging.Next == "" || after == "" {
			break
		}
		q.Set("after", after)
	}
	return s.res.Events, nil
}

// CountComments đọc số comment Meta đếm trên phiên live videoID (summary.total_count, đối chiếu cuối phiên).
func (c *Channel) CountComments(ctx context.Context, _ channel.ChannelAccount, cred channel.Credentials, videoID string) (int, error) {
	var out struct {
		Summary struct {
			TotalCount int `json:"total_count"`
		} `json:"summary"`
	}
	q := url.Values{"summary": {"total_count"}, "filter": {"stream"}, "limit": {"0"}}
	if _, err := c.client.Do(ctx, http.MethodGet, videoID+"/comments", cred.AccessToken, q, nil, &out); err != nil {
		return 0, err
	}
	return out.Summary.TotalCount, nil
}

// liveComment thêm comment live cm trên phiên videoID của Page page: bài là mã live (IsLiveComment của conversation
// khớp mã live; Meta đặt mã bài thế nào chưa xác minh, E4), cha là comment cha hay bài; khóa chống trùng và khóa thứ
// tự giống feed của webhook (Split) để ba đường của cùng comment chỉ thành một việc. Thiếu mã hay giờ → bỏ.
func (s *splitter) liveComment(page, videoID string, cm graphComment) {
	at := cm.CreatedTime.t
	if cm.ID == "" || at.IsZero() {
		s.skip(itemComment + ":" + skipInvalid)
		return
	}
	parent := videoID
	if cm.Parent != nil && cm.Parent.ID != "" {
		parent = cm.Parent.ID
	}
	s.add(channel.EventCommentChanged, "c:"+cm.ID+":add:"+textKey(cm.Message), "fbc:"+page+":"+videoID+":"+cm.From.ID,
		page, at, channel.CommentPayload{
			PageID: page, PostID: videoID, CommentID: cm.ID, ParentID: parent, FromID: cm.From.ID, FromName: cm.From.Name,
			Text: cm.Message, Verb: "add", FromPage: cm.From.ID == page, CreatedAt: at,
		})
}
