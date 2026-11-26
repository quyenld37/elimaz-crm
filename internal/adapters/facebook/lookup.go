package facebook

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
)

const (
	// lookupSlack: tra trả lời comment từ After − 60 s (lệch đồng hồ giữa CRM và Meta).
	lookupSlack = time.Minute
	// lookupLimit là số comment con tối đa đọc một lần tra.
	lookupLimit = 100
)

// Lookup hỏi Facebook một lần gửi "không rõ" đã tới chưa. Trả lời comment: đọc comment con sau After − 60 s, thấy
// comment của Page cùng nội dung → sent kèm mã; không thấy → unknown (Meta có thể trễ, không kết luận "chưa gửi").
// Ẩn/hiện: đọc is_hidden, đúng như muốn → sent, khác → not_sent (đặt lại an toàn). Tin và nhắn riêng → unknown không
// gọi Meta (đối chiếu echo cục bộ thuộc conversation). Lỗi Graph → unknown kèm lỗi.
func (c *Channel) Lookup(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials, q channel.LookupQuery) (channel.LookupResult, error) {
	unknown := channel.LookupResult{Status: channel.OutcomeUnknown}
	switch q.Purpose {
	case channel.PurposeCommentReply:
		return c.lookupReply(ctx, acc, cred, q)
	case channel.PurposeHide:
		if q.WantHidden == nil || q.CommentID == "" {
			return unknown, nil
		}
		var out struct {
			IsHidden bool `json:"is_hidden"`
		}
		if _, err := c.client.Do(ctx, http.MethodGet, q.CommentID, cred.AccessToken, url.Values{"fields": {"is_hidden"}}, nil, &out); err != nil {
			return unknown, err
		}
		if out.IsHidden == *q.WantHidden {
			return channel.LookupResult{Status: channel.OutcomeSent, ExternalID: q.CommentID}, nil
		}
		return channel.LookupResult{Status: channel.OutcomeNotSent}, nil
	case channel.PurposeMessage, channel.PurposePrivateReply:
	}
	return unknown, nil
}

func (c *Channel) lookupReply(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials, q channel.LookupQuery) (channel.LookupResult, error) {
	unknown := channel.LookupResult{Status: channel.OutcomeUnknown}
	if q.CommentID == "" {
		return unknown, nil
	}
	v := url.Values{
		"filter": {"stream"}, "since": {strconv.FormatInt(q.After.Add(-lookupSlack).Unix(), 10)},
		"fields": {"id,message,from,created_time"}, "limit": {strconv.Itoa(lookupLimit)},
	}
	var out struct {
		Data []struct {
			ID      string `json:"id"`
			Message string `json:"message"`
			From    idRef  `json:"from"`
		} `json:"data"`
	}
	if _, err := c.client.Do(ctx, http.MethodGet, q.CommentID+"/comments", cred.AccessToken, v, nil, &out); err != nil {
		return unknown, err
	}
	for _, cm := range out.Data {
		if cm.From.ID == acc.ExternalID && cm.Message == q.Text && cm.ID != "" {
			return channel.LookupResult{Status: channel.OutcomeSent, ExternalID: cm.ID}, nil
		}
	}
	return unknown, nil
}
