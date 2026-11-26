package facebook

import (
	"context"
	"net/http"
	"time"
	"unicode/utf8"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// Giới hạn nội dung một tin của Messenger, kiểm trước khi gọi Meta (spec 02 mục 3).
const (
	maxQuickReplies     = 13
	maxQuickReplyTitle  = 20
	maxQuickReplyLoad   = 1000
	maxMessageText      = 2000
	messagingTypeReply  = "RESPONSE"
	messagingTypeTag    = "MESSAGE_TAG"
	tagHumanAgent       = "HUMAN_AGENT"
	quickReplyKindText  = "text"
	attachmentKindImage = "image"
	// slowDownWait là thời gian giãn tối thiểu sau một lần gửi khi mức dùng của Meta từ 80% (tệp task-156, Quyết định 5).
	slowDownWait = time.Second
)

// Thân POST {page-id}/messages của Send API.
type (
	sendRecipient struct {
		ID        string `json:"id,omitempty"`
		CommentID string `json:"comment_id,omitempty"`
	}
	sendQuickReply struct {
		ContentType string `json:"content_type"`
		Title       string `json:"title"`
		Payload     string `json:"payload"`
	}
	sendAttachment struct {
		Type    string `json:"type"`
		Payload struct {
			URL        string `json:"url"`
			IsReusable bool   `json:"is_reusable"`
		} `json:"payload"`
	}
	sendMessage struct {
		Text         string           `json:"text,omitempty"`
		Attachment   *sendAttachment  `json:"attachment,omitempty"`
		QuickReplies []sendQuickReply `json:"quick_replies,omitempty"`
		Metadata     string           `json:"metadata,omitempty"`
	}
	sendRequest struct {
		Recipient     sendRecipient `json:"recipient"`
		MessagingType string        `json:"messaging_type"`
		Tag           string        `json:"tag,omitempty"`
		Message       sendMessage   `json:"message"`
	}
)

// Send gửi một lệnh ra Facebook bằng token Page cred: tin (POST {page-id}/messages, RESPONSE hay MESSAGE_TAG
// HUMAN_AGENT, metadata = ClientRef), nhắn riêng (cùng đường, recipient.comment_id), trả lời comment
// (POST {comment-id}/comments), ẩn/hiện comment (POST {comment-id} is_hidden). Lệnh sai (cả chữ lẫn ảnh, quá 13 nút,
// tiêu đề nút quá 20 ký tự…) hay không có token → rejected, không gọi Meta. Kết quả theo Classify: sent; rejected
// (lỗi chính sách kèm Reason; lỗi vĩnh viễn chỉ có Detail, Code); not_sent (chắc chắn chưa tới Meta, hoặc Meta giới
// hạn, kèm RetryAfter); unknown. Token hỏng → rejected no_permission và trả kèm lỗi gốc để bên gọi báo kênh hỏng
// (ClassifyError = auth); mọi ca khác lỗi là nil. Không tự gửi lại.
func (c *Channel) Send(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials, cmd channel.OutboundCommand) (channel.SendOutcome, error) {
	if cred.AccessToken == "" {
		return channel.SendOutcome{Status: channel.OutcomeRejected, Reason: channel.DenyNoPermission, Detail: "Page chưa có token"}, nil
	}
	if bad := validate(cmd); bad != "" {
		return channel.SendOutcome{Status: channel.OutcomeRejected, Detail: bad}, nil
	}
	switch cmd.Purpose {
	case channel.PurposeCommentReply:
		var out struct {
			ID string `json:"id"`
		}
		meta, err := c.client.Do(ctx, http.MethodPost, cmd.CommentID+"/comments", cred.AccessToken, nil, map[string]string{"message": cmd.Text}, &out)
		return sentOrError(meta, err, out.ID, "")
	case channel.PurposeHide:
		var out struct {
			Success bool `json:"success"`
		}
		meta, err := c.client.Do(ctx, http.MethodPost, cmd.CommentID, cred.AccessToken, nil, map[string]bool{"is_hidden": *cmd.Hidden}, &out)
		if err == nil && !out.Success {
			return channel.SendOutcome{Status: channel.OutcomeUnknown, Detail: "Facebook không xác nhận đã ẩn/hiện"}, nil
		}
		return sentOrError(meta, err, cmd.CommentID, "")
	case channel.PurposeMessage, channel.PurposePrivateReply:
	}
	var out struct {
		RecipientID string `json:"recipient_id"`
		MessageID   string `json:"message_id"`
	}
	meta, err := c.client.Do(ctx, http.MethodPost, acc.ExternalID+"/messages", cred.AccessToken, nil, messageBody(cmd), &out)
	return sentOrError(meta, err, out.MessageID, out.RecipientID)
}

// messageBody dựng thân Send API của tin hay nhắn riêng đã kiểm.
func messageBody(cmd channel.OutboundCommand) sendRequest {
	r := sendRequest{MessagingType: messagingTypeReply, Message: sendMessage{Text: cmd.Text, Metadata: cmd.ClientRef}}
	if cmd.Purpose == channel.PurposePrivateReply {
		r.Recipient.CommentID = cmd.CommentID
	} else {
		r.Recipient.ID = cmd.RecipientID
	}
	if cmd.Mode == channel.SendHumanAgent {
		r.MessagingType, r.Tag = messagingTypeTag, tagHumanAgent
	}
	if cmd.ImageURL != "" {
		a := &sendAttachment{Type: attachmentKindImage}
		a.Payload.URL = cmd.ImageURL
		r.Message.Attachment = a
	}
	for _, q := range cmd.QuickReplies {
		r.Message.QuickReplies = append(r.Message.QuickReplies, sendQuickReply{ContentType: quickReplyKindText, Title: q.Title, Payload: q.Payload})
	}
	return r
}

// validate trả câu tiếng Việt khi lệnh không gửi được theo giới hạn của Meta, rỗng khi hợp lệ.
func validate(cmd channel.OutboundCommand) string {
	switch cmd.Purpose {
	case channel.PurposeMessage:
		if cmd.RecipientID == "" {
			return "Thiếu người nhận"
		}
		if cmd.Mode != channel.SendStandard && cmd.Mode != channel.SendHumanAgent {
			return "Cách gửi không hợp lệ cho tin nhắn"
		}
		return validateContent(cmd)
	case channel.PurposePrivateReply:
		if cmd.CommentID == "" || cmd.Mode != channel.SendPrivateReply {
			return "Nhắn riêng cần comment và cách gửi nhắn riêng"
		}
		return validateContent(cmd)
	case channel.PurposeCommentReply:
		if cmd.CommentID == "" || cmd.Text == "" {
			return "Trả lời comment cần comment và nội dung"
		}
		return ""
	case channel.PurposeHide:
		if cmd.CommentID == "" || cmd.Hidden == nil {
			return "Ẩn/hiện comment cần comment và trạng thái muốn đặt"
		}
		return ""
	}
	return "Mục đích gửi không hợp lệ"
}

// validateContent: mỗi tin chỉ chữ hoặc một ảnh; chữ tối đa 2 000 ký tự; tối đa 13 nút trả lời nhanh, tiêu đề 1–20 ký
// tự, payload 1–1 000 ký tự.
func validateContent(cmd channel.OutboundCommand) string {
	switch {
	case (cmd.Text == "") == (cmd.ImageURL == ""):
		return "Mỗi tin chỉ gửi chữ hoặc một ảnh"
	case utf8.RuneCountInString(cmd.Text) > maxMessageText:
		return "Tin quá 2000 ký tự"
	case len(cmd.QuickReplies) > maxQuickReplies:
		return "Tối đa 13 nút trả lời nhanh"
	}
	for _, q := range cmd.QuickReplies {
		if n := utf8.RuneCountInString(q.Title); n == 0 || n > maxQuickReplyTitle {
			return "Tiêu đề nút trả lời nhanh phải có 1–20 ký tự"
		}
		if n := utf8.RuneCountInString(q.Payload); n == 0 || n > maxQuickReplyLoad {
			return "Mã nút trả lời nhanh phải có 1–1000 ký tự"
		}
	}
	return ""
}

// sentOrError: không lỗi → sent kèm mã của Meta (thiếu mã thì không chắc đã gửi → unknown) và, khi Meta báo mức dùng
// từ 80% hay thời gian chờ, RetryAfter = thời gian bên gọi nên giãn lần gửi sau (tối thiểu slowDownWait); lỗi → outcome.
func sentOrError(meta Meta, err error, externalID, recipientID string) (channel.SendOutcome, error) {
	if err != nil {
		return outcome(err)
	}
	if externalID == "" {
		return channel.SendOutcome{Status: channel.OutcomeUnknown, Detail: "Facebook không trả mã tin"}, nil
	}
	o := channel.SendOutcome{Status: channel.OutcomeSent, ExternalID: externalID, RecipientID: recipientID}
	if meta.Usage.ShouldSlowDown() {
		o.RetryAfter = max(meta.Usage.RegainAfter, slowDownWait)
	}
	return o, nil
}

// outcome đổi lỗi Graph sang kết quả gửi theo lớp (Classify); Detail là câu tiếng Việt của Reason (không mang message
// gốc của Meta). Chỉ lớp auth trả kèm lỗi.
func outcome(err error) (channel.SendOutcome, error) {
	reason, text := Reason(err)
	o := channel.SendOutcome{Detail: text}
	if ge, ok := asGraphError(err); ok {
		o.Code = ge.Code
	}
	switch Classify(err) {
	case channel.ErrorPolicy:
		o.Status, o.Reason = channel.OutcomeRejected, reason
	case channel.ErrorAuth:
		o.Status, o.Reason = channel.OutcomeRejected, channel.DenyNoPermission
		return o, err
	case channel.ErrorPermanent:
		o.Status = channel.OutcomeRejected
	case channel.ErrorRateLimited:
		o.Status, o.RetryAfter = channel.OutcomeNotSent, RetryAfter(err)
	case channel.ErrorNotSentRetry:
		o.Status = channel.OutcomeNotSent
	case channel.ErrorUnknown:
		o.Status = channel.OutcomeUnknown
	}
	return o, nil
}
