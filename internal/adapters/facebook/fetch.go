package facebook

import (
	"context"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

const (
	// historyDefault là số tin gần nhất đọc khi có hội thoại mới (Q33); historyMax là trần một lần đọc.
	historyDefault = 20
	historyMax     = 100
	// backfillPages là số trang hội thoại tối đa một lần hỏi bù; backfillConversations là số hội thoại mỗi trang.
	backfillPages         = 10
	backfillConversations = 25
	backfillPosts         = 25
	backfillComments      = 100
	// snippetRunes là độ dài tối đa đoạn trích nội dung bài.
	snippetRunes = 200
	// messageFields là các trường của một tin khi đọc qua Conversations API.
	messageFields = "{id,message,from,to,created_time,attachments}"
)

// FetchProfile đọc tên và ảnh đại diện của PSID userID (GET {psid}?fields=first_name,last_name,profile_pic).
func (c *Channel) FetchProfile(ctx context.Context, _ channel.ChannelAccount, cred channel.Credentials, userID string) (channel.Profile, error) {
	var out struct {
		FirstName  string `json:"first_name"`
		LastName   string `json:"last_name"`
		ProfilePic string `json:"profile_pic"`
	}
	q := url.Values{"fields": {"first_name,last_name,profile_pic"}}
	if _, err := c.client.Do(ctx, http.MethodGet, userID, cred.AccessToken, q, nil, &out); err != nil {
		return channel.Profile{}, err
	}
	return channel.Profile{Name: strings.TrimSpace(out.FirstName + " " + out.LastName), AvatarURL: out.ProfilePic}, nil
}

// FetchPost đọc bài viết postID (đường dẫn, nội dung trích tối đa 200 ký tự, giờ đăng, loại).
func (c *Channel) FetchPost(ctx context.Context, _ channel.ChannelAccount, cred channel.Credentials, postID string) (channel.PostInfo, error) {
	var out struct {
		ID          string   `json:"id"`
		Permalink   string   `json:"permalink_url"`
		Message     string   `json:"message"`
		CreatedTime flexTime `json:"created_time"`
		StatusType  string   `json:"status_type"`
	}
	q := url.Values{"fields": {"permalink_url,message,created_time,status_type"}}
	if _, err := c.client.Do(ctx, http.MethodGet, postID, cred.AccessToken, q, nil, &out); err != nil {
		return channel.PostInfo{}, err
	}
	id := out.ID
	if id == "" {
		id = postID
	}
	return channel.PostInfo{
		ID: id, Kind: out.StatusType, Permalink: out.Permalink, Snippet: truncate(out.Message, snippetRunes),
		CreatedTime: out.CreatedTime.t,
	}, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Download tải tệp đính kèm, chỉ từ máy chủ của Meta (Client.Download).
func (c *Channel) Download(ctx context.Context, rawURL string, maxBytes int64) (io.ReadCloser, channel.DownloadInfo, error) {
	return c.client.Download(ctx, rawURL, maxBytes)
}

// Tin đọc qua Conversations API.
type (
	graphAttachment struct {
		MimeType  string `json:"mime_type"`
		Name      string `json:"name"`
		FileURL   string `json:"file_url"`
		ImageData *struct {
			URL string `json:"url"`
		} `json:"image_data"`
		VideoData *struct {
			URL string `json:"url"`
		} `json:"video_data"`
	}
	graphMessage struct {
		ID          string   `json:"id"`
		Message     string   `json:"message"`
		CreatedTime flexTime `json:"created_time"`
		From        idRef    `json:"from"`
		To          struct {
			Data []idRef `json:"data"`
		} `json:"to"`
		Attachments struct {
			Data []graphAttachment `json:"data"`
		} `json:"attachments"`
	}
	graphConversation struct {
		ID          string   `json:"id"`
		UpdatedTime flexTime `json:"updated_time"`
		Messages    struct {
			Data []graphMessage `json:"data"`
		} `json:"messages"`
	}
	conversationPage struct {
		Data   []graphConversation `json:"data"`
		Paging struct {
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
			Next string `json:"next"`
		} `json:"paging"`
	}
)

// FetchHistory đọc tối đa limit tin gần nhất (mặc định 20, trần 100) giữa Page và PSID userID, trả theo thứ tự thời
// gian: tin khách là message.received, tin Page là message.echo; khóa chống trùng, khóa thứ tự như webhook (m:<mid>,
// fb:<page>:<psid>) nên tin đã vào qua webhook không tạo việc thứ hai. Bên gọi ghi bằng webhooks.EnqueueInbound với
// via = history.
func (c *Channel) FetchHistory(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials, userID string, limit int) ([]webhooks.InboundEvent, error) {
	if limit <= 0 {
		limit = historyDefault
	}
	limit = min(limit, historyMax)
	q := url.Values{"user_id": {userID}, "fields": {"messages.limit(" + strconv.Itoa(limit) + ")" + messageFields}}
	var out conversationPage
	if _, err := c.client.Do(ctx, http.MethodGet, acc.ExternalID+"/conversations", cred.AccessToken, q, nil, &out); err != nil {
		return nil, err
	}
	var msgs []graphMessage
	for _, conv := range out.Data {
		msgs = append(msgs, conv.Messages.Data...)
	}
	// Meta trả tin mới nhất trước: giữ limit tin đầu rồi xếp lại theo thời gian.
	msgs = msgs[:min(len(msgs), limit)]
	return messageEvents(acc.ExternalID, msgs, time.Time{}), nil
}

// messageEvents đổi tin Graph thành sự kiện (bỏ tin không sau since, thiếu mã, người, giờ), xếp theo thời gian.
func messageEvents(page string, msgs []graphMessage, since time.Time) []webhooks.InboundEvent {
	s := &splitter{res: webhooks.SplitResult{Skipped: map[string]int{}}}
	slices.SortStableFunc(msgs, func(a, b graphMessage) int { return a.CreatedTime.t.Compare(b.CreatedTime.t) })
	for _, m := range msgs {
		at := m.CreatedTime.t
		echo := m.From.ID == page
		user := m.From.ID
		if echo {
			user = ""
			if len(m.To.Data) > 0 {
				user = m.To.Data[0].ID
			}
		}
		if m.ID == "" || user == "" || at.IsZero() || !at.After(since) {
			continue
		}
		typ := channel.EventMessageReceived
		if echo {
			typ = channel.EventMessageEcho
		}
		s.add(typ, "m:"+m.ID, "fb:"+page+":"+user, page, at, channel.MessagePayload{
			PageID: page, UserID: user, MID: m.ID, Text: m.Message, IsEcho: echo, SentAt: at,
			Attachments: graphAttachments(m.Attachments.Data),
		})
	}
	return s.res.Events
}

// graphAttachments chuẩn hóa tệp đính kèm đọc qua Graph (image_data, video_data, mime audio/*, file_url).
func graphAttachments(in []graphAttachment) []channel.AttachmentRef {
	var out []channel.AttachmentRef
	for _, a := range in {
		r := channel.AttachmentRef{Type: "file", URL: a.FileURL, Name: a.Name}
		switch {
		case a.ImageData != nil:
			r.Type, r.URL = "image", a.ImageData.URL
		case a.VideoData != nil:
			r.Type, r.URL = "video", a.VideoData.URL
		case strings.HasPrefix(a.MimeType, "audio/"):
			r.Type = "audio"
		case a.FileURL == "":
			r.Type = "fallback"
		}
		out = append(out, r)
	}
	return out
}

// Backfill đọc lại sự kiện sau since khi webhook im lặng: tin của các hội thoại cập nhật sau since (Conversations API,
// tối đa 10 trang, dừng khi gặp hội thoại cũ hơn since) rồi comment sau since trên các bài gần nhất (feed). Khóa chống
// trùng như webhook nên sự kiện đã vào không tạo việc thứ hai. Giãn tối đa 2 lời gọi mỗi giây mỗi Page. Lỗi Graph hay
// ctx bị hủy → trả lỗi một lần rồi dừng. Bên gọi ghi bằng webhooks.EnqueueInbound với via = backfill.
func (c *Channel) Backfill(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials, since time.Time) iter.Seq2[webhooks.InboundEvent, error] {
	return func(yield func(webhooks.InboundEvent, error) bool) {
		b := &backfill{c: c, acc: acc, cred: cred, since: since}
		evs, err := b.conversations(ctx)
		if err == nil {
			var more []webhooks.InboundEvent
			more, err = b.feed(ctx)
			evs = append(evs, more...)
		}
		for _, e := range evs {
			if !yield(e, nil) {
				return
			}
		}
		if err != nil {
			yield(webhooks.InboundEvent{}, err)
		}
	}
}

// PollPageComments đọc comment sau since trên các bài gần nhất của Page (một lời gọi feed, như phần comment của
// Backfill) để đọc comment định kỳ khi webhook feed không về (Q4). Bên gọi ghi bằng webhooks.EnqueueInbound với via =
// comment_poll.
func (c *Channel) PollPageComments(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials, since time.Time,
) ([]webhooks.InboundEvent, error) {
	b := &backfill{c: c, acc: acc, cred: cred, since: since}
	return b.feed(ctx)
}

// backfill là một lần hỏi bù của một Page: giữ mốc lời gọi trước để giãn lời gọi.
type backfill struct {
	c     *Channel
	acc   channel.ChannelAccount
	cred  channel.Credentials
	since time.Time
	last  time.Time
}

// get gọi Graph sau khi chờ đủ khoảng giãn kể từ lời gọi trước.
func (b *backfill) get(ctx context.Context, path string, q url.Values, out any) error {
	if !b.last.IsZero() {
		t := time.NewTimer(time.Until(b.last.Add(b.c.backfillGap)))
		select {
		case <-ctx.Done():
			t.Stop()
			return fmt.Errorf("hỏi bù dừng: %w", context.Cause(ctx))
		case <-t.C:
		}
	}
	b.last = time.Now()
	_, err := b.c.client.Do(ctx, http.MethodGet, path, b.cred.AccessToken, q, nil, out)
	return err
}

func (b *backfill) conversations(ctx context.Context) ([]webhooks.InboundEvent, error) {
	page := b.acc.ExternalID
	q := url.Values{
		"fields": {"updated_time,messages.limit(" + strconv.Itoa(historyDefault) + ")" + messageFields},
		"limit":  {strconv.Itoa(backfillConversations)},
	}
	var evs []webhooks.InboundEvent
	for range backfillPages {
		var out conversationPage
		if err := b.get(ctx, page+"/conversations", q, &out); err != nil {
			return evs, err
		}
		older := false
		for _, conv := range out.Data {
			if u := conv.UpdatedTime.t; !u.IsZero() && !u.After(b.since) {
				older = true // Meta xếp hội thoại theo giờ cập nhật giảm dần: từ đây trở đi không có gì mới
				break
			}
			evs = append(evs, messageEvents(page, conv.Messages.Data, b.since)...)
		}
		after := out.Paging.Cursors.After
		if older || out.Paging.Next == "" || after == "" {
			break
		}
		q.Set("after", after)
	}
	return evs, nil
}

type graphComment struct {
	ID          string   `json:"id"`
	Message     string   `json:"message"`
	CreatedTime flexTime `json:"created_time"`
	From        struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"from"`
	Parent *idRef `json:"parent"`
}

// feed đọc comment sau since trên các bài gần nhất của Page thành comment.changed (verb add).
func (b *backfill) feed(ctx context.Context) ([]webhooks.InboundEvent, error) {
	page := b.acc.ExternalID
	sinceUnix := strconv.FormatInt(b.since.Unix(), 10)
	q := url.Values{
		"fields": {"id,comments.filter(stream).since(" + sinceUnix + ").limit(" + strconv.Itoa(backfillComments) +
			"){id,message,from,created_time,parent{id}}"},
		"limit": {strconv.Itoa(backfillPosts)},
	}
	var out struct {
		Data []struct {
			ID       string `json:"id"`
			Comments struct {
				Data []graphComment `json:"data"`
			} `json:"comments"`
		} `json:"data"`
	}
	if err := b.get(ctx, page+"/feed", q, &out); err != nil {
		return nil, err
	}
	s := &splitter{res: webhooks.SplitResult{Skipped: map[string]int{}}}
	for _, post := range out.Data {
		for _, cm := range post.Comments.Data {
			at := cm.CreatedTime.t
			if cm.ID == "" || post.ID == "" || !at.After(b.since) {
				continue
			}
			parent := post.ID
			if cm.Parent != nil && cm.Parent.ID != "" {
				parent = cm.Parent.ID
			}
			s.add(channel.EventCommentChanged, "c:"+cm.ID+":add:"+textKey(cm.Message), "fbc:"+page+":"+post.ID+":"+cm.From.ID,
				page, at, channel.CommentPayload{
					PageID: page, PostID: post.ID, CommentID: cm.ID, ParentID: parent, FromID: cm.From.ID,
					FromName: cm.From.Name, Text: cm.Message, Verb: "add", FromPage: cm.From.ID == page, CreatedAt: at,
				})
		}
	}
	return s.res.Events, nil
}
