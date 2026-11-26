package inbound

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Comment vào (Task 154, CH02, CH13): comment của khách vào hội thoại kind comment của (Page, bài, người); comment của
// chính Page vào hội thoại của comment cha (tin out page_external). Sửa, xóa, ẩn, hiện ghi message_revisions (nội dung
// trước khi đổi, trạng thái ẩn/xóa sau khi đổi) rồi đổi dòng tin; tới trước add thì chèn tin với trạng thái đó, add đến
// sau đụng khóa tự nhiên nên không ghi đè (tệp task-154, Quyết định 2–4).

// Verb của channel.CommentPayload.
const (
	verbAdd    = "add"
	verbEdited = "edited"
	verbRemove = "remove"
	verbHide   = "hide"
	verbUnhide = "unhide"
)

// commentState là trạng thái một comment: nội dung, mốc sửa, mốc xóa, ẩn.
type commentState struct {
	body              pgtype.Text
	editedAt, removed pgtype.Timestamptz
	hidden            bool
}

// comment xử lý một sự kiện comment. Verb lạ → inbound_ignored{comment_verb}; không có người comment →
// inbound_ignored{comment_without_from}; Page không có trong shop → bỏ qua.
// Sửa/xóa/ẩn/hiện comment đã có: chỉ khóa dòng tin (không chạm hội thoại). Còn lại: đường tạo (khách → bài → hội
// thoại → tin, ADR-46 nhóm 5 rồi 6).
func (h *handler) comment(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, p channel.CommentPayload) error {
	if p.CommentID == "" || p.PostID == "" || p.CreatedAt.IsZero() {
		return fmt.Errorf("comment thiếu mã hay mốc: %w", queue.ErrPermanent)
	}
	if p.FromID == "" { // Meta không gửi người comment (thiếu quyền đọc nội dung người dùng): không gắn được khách
		h.ignore("comment_without_from")
		return nil
	}
	switch p.Verb {
	case verbAdd, verbEdited, verbRemove, verbHide, verbUnhide:
	default:
		h.ignore("comment_verb")
		return nil
	}
	acc, ok, err := h.account(ctx, tx, e, p.PageID)
	if err != nil || !ok {
		return err
	}
	if p.Verb != verbAdd {
		row, err := store.New(tx).LockComment(ctx, store.LockCommentParams{ChannelAccountID: acc.ID, ExternalID: text(p.CommentID)})
		switch {
		case err == nil:
			return h.applyComment(ctx, tx, e, acc, p, row)
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("khóa comment: %w", err)
		}
	}
	return h.insertComment(ctx, tx, e, acc, p)
}

// insertComment ghi comment chưa có (add, hay sự kiện đổi trạng thái tới trước add).
func (h *handler) insertComment(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, acc channel.ChannelAccount,
	p channel.CommentPayload,
) error {
	at := p.CreatedAt.UTC()
	convID, postID, ok, err := h.commentThread(ctx, tx, e, acc, p, at)
	if err != nil || !ok {
		return err
	}
	st := initialState(p, at)
	live, err := isLiveComment(ctx, tx, postID, at)
	if err != nil {
		return err
	}
	var refs []channel.AttachmentRef
	if p.PhotoURL != "" {
		refs = []channel.AttachmentRef{{Type: "image", URL: p.PhotoURL}}
	}
	atts, err := deps.PendingAttachments(refs)
	if err != nil {
		return err //nolint:wrapcheck // deps đã bọc việc đang làm
	}
	dir, origin := dirIn, originCustomer
	if p.FromPage {
		dir, origin = dirOut, originPageExternal
	}
	q := store.New(tx)
	msgID, err := q.InsertComment(ctx, store.InsertCommentParams{
		ConversationID: convID, ChannelAccountID: acc.ID, ExternalID: text(p.CommentID), Direction: dir, Origin: origin,
		ParentExternalID: text(parentRef(p)), PostID: pgtype.UUID{Bytes: postID, Valid: true}, Body: st.body,
		Attachments: atts, SenderName: text(p.FromName), IsHidden: st.hidden, IsLive: live, RemovedAt: st.removed,
		EditedAt: st.editedAt, SentAt: at,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if p.Verb == verbAdd {
			return nil // trùng, hay add đến sau sự kiện đổi trạng thái: không ghi đè
		}
		// add song song vừa chèn trước: khóa tin (sau hội thoại, đúng thứ tự) rồi áp đổi.
		row, err := q.LockComment(ctx, store.LockCommentParams{ChannelAccountID: acc.ID, ExternalID: text(p.CommentID)})
		if err != nil {
			return fmt.Errorf("khóa comment vừa chèn: %w", err)
		}
		return h.applyComment(ctx, tx, e, acc, p, row)
	}
	if err != nil {
		return fmt.Errorf("ghi comment vào: %w", err)
	}
	return h.afterCommentInserted(ctx, tx, e, acc, p, msgID, convID, st, refs, live)
}

// afterCommentInserted là phần sau khi chèn comment mới msgID: dòng lịch sử của sự kiện tới trước add (không biết nội
// dung trước đó: body NULL), cập nhật hội thoại, đính kèm, hook tự động và live (comment khách chưa xóa), báo hiệu.
func (h *handler) afterCommentInserted(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, acc channel.ChannelAccount,
	p channel.CommentPayload, msgID, convID uuid.UUID, st commentState, refs []channel.AttachmentRef, live bool,
) error {
	if p.Verb != verbAdd {
		if err := h.revision(ctx, tx, p, msgID, pgtype.Text{}, st); err != nil {
			return err
		}
	}
	if err := h.bumpForComment(ctx, tx, p, convID, st, p.CreatedAt.UTC()); err != nil {
		return err
	}
	if len(refs) > 0 {
		if err := OnInboundAttachments(ctx, tx, msgID, refs); err != nil {
			return err
		}
	}
	if !p.FromPage && !st.removed.Valid {
		if err := onCommentCreated(ctx, tx, msgID); err != nil {
			return err
		}
		if live {
			if err := onLiveComment(ctx, tx, msgID); err != nil {
				return err
			}
		}
	}
	return h.publish(ctx, tx, e, acc, convID)
}

// bumpForComment cập nhật hội thoại đã khóa sau comment vừa chèn: comment khách như tin vào (chưa đọc + 1), comment
// Page như tin Page gửi ngoài CRM; comment đã xóa (xóa tới trước add) không đổi hội thoại.
func (h *handler) bumpForComment(ctx context.Context, tx pgx.Tx, p channel.CommentPayload, convID uuid.UUID,
	st commentState, at time.Time,
) error {
	if st.removed.Valid {
		return nil
	}
	q := store.New(tx)
	snip := text(deps.Snippet(p.Text, p.PhotoURL != ""))
	if p.FromPage {
		if err := q.BumpConversationOutbound(ctx, store.BumpConversationOutboundParams{At: at, Snippet: snip, ID: convID}); err != nil {
			return fmt.Errorf("cập nhật hội thoại sau comment của Page: %w", err)
		}
		return nil
	}
	if _, err := q.BumpConversationInbound(ctx, store.BumpConversationInboundParams{At: at, Snippet: snip, ID: convID}); err != nil {
		return fmt.Errorf("cập nhật hội thoại sau comment: %w", err)
	}
	return nil
}

// commentThread trả hội thoại (đã khóa) và bài của comment p. Khách: tìm hoặc tạo khách theo PSID (tên thật từ comment)
// → bài → hội thoại comment (Page, bài, người). Page: hội thoại của comment cha (không khóa) → bài → khóa hội thoại;
// không có hội thoại (comment gốc của Page, cha chưa có) → ok = false, đếm inbound_ignored{page_comment_no_thread}.
func (h *handler) commentThread(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, acc channel.ChannelAccount,
	p channel.CommentPayload, at time.Time,
) (convID, postID uuid.UUID, ok bool, err error) {
	if p.FromPage {
		return h.pageCommentThread(ctx, tx, e, acc, p)
	}
	cc, err := h.customerFor(ctx, tx, acc, p.FromID, p.FromName)
	if err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	if postID, err = h.ensurePost(ctx, tx, e, acc, p.PostID); err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	row, err := store.New(tx).UpsertCommentConversation(ctx, store.UpsertCommentConversationParams{
		ChannelAccountID: acc.ID, CustomerID: cc.CustomerID, ExternalThreadRef: p.PostID + ":" + p.FromID,
		PostID: pgtype.UUID{Bytes: postID, Valid: true}, At: at,
	})
	if err != nil {
		return uuid.Nil, uuid.Nil, false, fmt.Errorf("ghi hội thoại comment: %w", err)
	}
	if cc.Created {
		if err := OnNewCustomer(ctx, tx, cc.CustomerID, row.ID); err != nil {
			return uuid.Nil, uuid.Nil, false, err
		}
	}
	if row.Inserted {
		if err := onNewConversation(ctx, tx, row.ID); err != nil {
			return uuid.Nil, uuid.Nil, false, err
		}
	}
	return row.ID, postID, true, nil
}

// pageCommentThread là commentThread cho comment của Page: hội thoại của comment cha (không khóa) → bài → khóa hội
// thoại; không có hội thoại → ok = false, đếm inbound_ignored{page_comment_no_thread}.
func (h *handler) pageCommentThread(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, acc channel.ChannelAccount,
	p channel.CommentPayload,
) (convID, postID uuid.UUID, ok bool, err error) {
	q := store.New(tx)
	parent := parentRef(p)
	if parent != "" {
		convID, err = q.FindCommentConversation(ctx, store.FindCommentConversationParams{ChannelAccountID: acc.ID, ExternalID: text(parent)})
	}
	if parent == "" || errors.Is(err, pgx.ErrNoRows) {
		h.ignore("page_comment_no_thread")
		return uuid.Nil, uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, false, fmt.Errorf("tìm hội thoại của comment cha: %w", err)
	}
	if postID, err = h.ensurePost(ctx, tx, e, acc, p.PostID); err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	if err := q.LockConversation(ctx, convID); err != nil {
		return uuid.Nil, uuid.Nil, false, fmt.Errorf("khóa hội thoại comment: %w", err)
	}
	return convID, postID, true, nil
}

// applyComment áp sự kiện p lên comment đã khóa row: đổi thật thì ghi lịch sử (nội dung cũ) rồi đổi dòng và báo hiệu;
// giao lại, bản sửa cũ hơn, xóa khi đã xóa, ẩn khi đã ẩn, add đến muộn → không làm gì.
func (h *handler) applyComment(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, acc channel.ChannelAccount,
	p channel.CommentPayload, row store.LockCommentRow,
) error {
	at := p.CreatedAt.UTC()
	next := commentState{body: row.Body, editedAt: row.EditedAt, removed: row.RemovedAt, hidden: row.IsHidden}
	switch p.Verb {
	case verbEdited:
		if row.Body == text(p.Text) || (row.EditedAt.Valid && !row.EditedAt.Time.Before(at)) {
			return nil
		}
		next.body, next.editedAt = text(p.Text), pgtype.Timestamptz{Time: at, Valid: true}
	case verbRemove:
		if row.RemovedAt.Valid {
			return nil
		}
		next.removed = pgtype.Timestamptz{Time: at, Valid: true}
	case verbHide, verbUnhide:
		if row.IsHidden == (p.Verb == verbHide) {
			return nil
		}
		next.hidden = p.Verb == verbHide
	default:
		return nil
	}
	if err := h.revision(ctx, tx, p, row.ID, row.Body, next); err != nil {
		return err
	}
	if err := store.New(tx).UpdateCommentState(ctx, store.UpdateCommentStateParams{
		Body: next.body, EditedAt: next.editedAt, RemovedAt: next.removed, IsHidden: next.hidden, ID: row.ID,
	}); err != nil {
		return fmt.Errorf("đổi trạng thái comment: %w", err)
	}
	return h.publish(ctx, tx, e, acc, row.ConversationID)
}

// revision ghi một dòng lịch sử của comment msgID: nội dung trước khi đổi (old), trạng thái ẩn/xóa sau khi đổi (st).
// Nguồn: comment của Page, hay ẩn/hiện (Page kiểm duyệt) → page_external; sửa, xóa comment của khách → customer.
func (h *handler) revision(ctx context.Context, tx pgx.Tx, p channel.CommentPayload, msgID uuid.UUID, old pgtype.Text,
	st commentState,
) error {
	source := originCustomer
	if p.FromPage || p.Verb == verbHide || p.Verb == verbUnhide {
		source = originPageExternal
	}
	if err := store.New(tx).InsertRevisionState(ctx, store.InsertRevisionStateParams{
		MessageID: msgID, Body: old, IsHidden: pgtype.Bool{Bool: st.hidden, Valid: true},
		Removed: pgtype.Bool{Bool: st.removed.Valid, Valid: true}, Source: source,
	}); err != nil {
		return fmt.Errorf("ghi lịch sử comment: %w", err)
	}
	return nil
}

// initialState là trạng thái của comment chèn lần đầu bởi sự kiện p (add, hay sự kiện đổi trạng thái tới trước add).
func initialState(p channel.CommentPayload, at time.Time) commentState {
	st := commentState{body: text(p.Text), hidden: p.IsHidden || p.Verb == verbHide}
	switch p.Verb {
	case verbEdited:
		st.editedAt = pgtype.Timestamptz{Time: at, Valid: true}
	case verbRemove:
		st.removed = pgtype.Timestamptz{Time: at, Valid: true}
	case verbUnhide:
		st.hidden = false
	}
	return st
}

// parentRef là mã comment cha của p; comment gốc của bài (cha là chính bài, hay rỗng) → rỗng.
func parentRef(p channel.CommentPayload) string {
	if p.ParentID == p.PostID {
		return ""
	}
	return p.ParentID
}
