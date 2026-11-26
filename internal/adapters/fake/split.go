package fake

import (
	"encoding/json"
	"fmt"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Event là một sự kiện trong gói webhook kênh giả. Type: message, echo, delivered, read, reaction, edit, comment,
// live, policy; loại khác vào Skipped. ID duy nhất mỗi sự kiện (khóa chống trùng).
type Event struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	PageID     string   `json:"pageId"`
	UserID     string   `json:"userId,omitempty"`
	MID        string   `json:"mid,omitempty"`
	MIDs       []string `json:"mids,omitempty"`
	Text       string   `json:"text,omitempty"`
	QuickReply string   `json:"quickReply,omitempty"`
	Metadata   string   `json:"metadata,omitempty"`
	// AppID là ứng dụng đã gửi tin của echo: "fakechat" (khóa kênh giả) = tin của CRM; rỗng hay khác = Page gửi ngoài CRM
	// (cùng quy ước với đường vào, tệp task-153 Quyết định 3).
	AppID       string    `json:"appId,omitempty"`
	Reaction    string    `json:"reaction,omitempty"`
	PostID      string    `json:"postId,omitempty"`
	CommentID   string    `json:"commentId,omitempty"`
	ParentID    string    `json:"parentId,omitempty"`
	Verb        string    `json:"verb,omitempty"`
	FromName    string    `json:"fromName,omitempty"`
	FromPage    bool      `json:"fromPage,omitempty"`
	LiveVideoID string    `json:"liveVideoId,omitempty"`
	Status      string    `json:"status,omitempty"`
	Action      string    `json:"action,omitempty"`
	At          time.Time `json:"at"`
}

// Packet là gói webhook {"events":[…]}.
type Packet struct {
	Events []Event `json:"events"`
}

// Split tách gói thành sự kiện chuẩn hóa: DedupeKey fakechat:<id>; OrderKey theo hội thoại (tin: Page + người dùng;
// comment: Page + bài; live: Page + video; chính sách: Page); Route{RouteKind, PageID}. Sự kiện thiếu id, pageId hay
// thời điểm vào Skipped["invalid"]; loại lạ vào Skipped[<loại>]. Thân không phải JSON → lỗi.
func (*Chat) Split(raw []byte) (webhooks.SplitResult, error) {
	var p Packet
	if err := json.Unmarshal(raw, &p); err != nil {
		return webhooks.SplitResult{}, fmt.Errorf("đọc gói kênh giả: %w", err)
	}
	out := webhooks.SplitResult{Skipped: map[string]int{}}
	for _, e := range p.Events {
		if e.ID == "" || e.PageID == "" || e.At.IsZero() {
			out.Skipped["invalid"]++
			continue
		}
		typ, order, payload, ok := normalize(e)
		if !ok {
			out.Skipped[e.Type]++
			continue
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return webhooks.SplitResult{}, fmt.Errorf("mã hóa sự kiện kênh giả: %w", err)
		}
		out.Events = append(out.Events, webhooks.InboundEvent{
			Type: typ, DedupeKey: Key + ":" + e.ID, Route: webhooks.Route{Kind: RouteKind, ExternalKey: e.PageID},
			OrderKey: Key + ":" + e.PageID + ":" + order, OccurredAt: e.At.UTC(), Payload: body,
		})
	}
	return out, nil
}

// normalize đổi một sự kiện giả thành (loại chuẩn hóa, phần sau của OrderKey, payload); ok = false khi loại lạ.
func normalize(e Event) (typ, order string, payload any, ok bool) {
	at := e.At.UTC()
	msg := func(echo bool) channel.MessagePayload {
		p := channel.MessagePayload{
			PageID: e.PageID, UserID: e.UserID, MID: e.MID, Text: e.Text, QuickReplyPayload: e.QuickReply,
			Metadata: e.Metadata, IsEcho: echo, SentAt: at,
		}
		if echo {
			p.EchoAppID = e.AppID
		}
		return p
	}
	user := "user:" + e.UserID
	switch e.Type {
	case "message":
		return channel.EventMessageReceived, user, msg(false), true
	case "echo":
		return channel.EventMessageEcho, user, msg(true), true
	case "delivered", "read":
		t := map[string]string{"delivered": channel.EventMessageDelivered, "read": channel.EventMessageRead}[e.Type]
		return t, user, channel.WatermarkPayload{PageID: e.PageID, UserID: e.UserID, MIDs: e.MIDs, Watermark: at}, true
	case "reaction":
		return channel.EventMessageReaction, user, channel.ReactionPayload{
			PageID: e.PageID, UserID: e.UserID, MID: e.MID, Reaction: e.Reaction, Action: e.Action,
		}, true
	case "edit":
		return channel.EventMessageEdited, user, channel.EditPayload{PageID: e.PageID, UserID: e.UserID, MID: e.MID, Text: e.Text, EditedAt: at}, true
	case "comment":
		return channel.EventCommentChanged, "post:" + e.PostID, channel.CommentPayload{
			PageID: e.PageID, PostID: e.PostID, CommentID: e.CommentID, ParentID: e.ParentID, FromID: e.UserID,
			FromName: e.FromName, Text: e.Text, Verb: e.Verb, FromPage: e.FromPage, CreatedAt: at,
		}, true
	case "live":
		return channel.EventLiveStatus, "live:" + e.LiveVideoID, channel.LiveStatusPayload{
			PageID: e.PageID, LiveVideoID: e.LiveVideoID, VideoID: e.LiveVideoID, Status: e.Status, At: at,
		}, true
	case "policy":
		return channel.EventPolicyEnforcement, "policy", channel.PolicyPayload{PageID: e.PageID, Action: e.Action}, true
	}
	return "", "", nil, false
}
