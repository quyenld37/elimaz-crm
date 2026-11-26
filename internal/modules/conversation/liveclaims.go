package conversation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/live"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/templates"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Chốt đơn từ comment live (Task 172; LV03, LV07, LV08, LV09; Q1: conversation điều phối, gọi public API của order).
// Hiện thực ở internal/live (claim.go, context.go, comments.go); xin địa chỉ ở internal/send (ask.go).

// Cách chốt và kết quả xin địa chỉ.
const (
	LiveClaimNew        = live.ModeNew
	LiveClaimAddToDraft = live.ModeAddToDraft
	LiveAskNotRequested = live.AskNotRequested
	// LiveCommentsMaxLimit là cỡ trang tối đa của danh sách comment live.
	LiveCommentsMaxLimit = live.CommentsMaxLimit
)

// LiveClaimItem là một dòng chốt (biến thể, số lượng 1–20).
type LiveClaimItem = live.ClaimItem

// LiveClaimInput là một lần bấm chốt comment.
type LiveClaimInput = live.ClaimInput

// LiveClaimResult là kết quả chốt: đơn, ghi nhận, xin địa chỉ; Before, Fields khi thêm vào nháp (audit).
type LiveClaimResult = live.ClaimResult

// LiveClaimRef là dòng ghi nhận (người chốt, đơn, mã đơn).
type LiveClaimRef = live.ClaimRef

// LiveDraft là đơn nháp của khách trong phiên live.
type LiveDraft = live.Draft

// LiveCommentQuery là bộ lọc danh sách comment của phiên live.
type LiveCommentQuery = live.CommentQuery

// LiveCommentRow là một comment của phiên live.
type LiveCommentRow = live.CommentRow

// CommentClaimedError là chi tiết của ErrCommentClaimed (409 comment_claimed kèm claimedBy, orderId, orderCode).
type CommentClaimedError = live.CommentClaimedError

// OpenDraftExistsError là chi tiết của ErrOpenDraftExists (409 open_draft_exists kèm drafts).
type OpenDraftExistsError = live.OpenDraftExistsError

// Lỗi của đường chốt comment live.
var (
	ErrClaimInvalid    = live.ErrClaimInvalid    // 400
	ErrCommentClaimed  = live.ErrCommentClaimed  // 409 comment_claimed
	ErrOpenDraftExists = live.ErrOpenDraftExists // 409 open_draft_exists
	ErrSellerMismatch  = live.ErrSellerMismatch  // 422 seller_mismatch (ADR-29)
	ErrDraftNotOpen    = live.ErrDraftNotOpen    // 409: đơn đã không còn nháp
	ErrDraftMismatch   = live.ErrDraftMismatch   // 422: đơn không của khách trong phiên
	ErrCursorInvalid   = live.ErrCursorInvalid   // 400
)

// LiveCommentContext là bối cảnh chốt comment: thẻ khách (như cạnh khung chat), đơn nháp của khách trong phiên, SĐT
// tách từ comment, người bán của Page.
type LiveCommentContext struct {
	Card       Card
	OpenDrafts []LiveDraft
	Phones     []string
	SellerID   uuid.UUID
}

// ClaimLiveComment chốt comment live commentID bằng nhân viên u (xem live.Claim), trong tx tenant của bên gọi: cần
// live.operate trên Page của comment (không có → ErrNotFound); a là người làm trên đơn (lớp HTTP chép từ u, đã kiểm
// order.create / order.update); w là đường ghi đơn. Nguồn đơn tự điền theo khung giờ live chứa giờ bắt đầu của phiên
// (giờ Việt Nam; không khung nào → nguồn comment; nguồn đã tắt → không điền); mẫu xin địa chỉ mặc định là
// ask_address_template_id của Page.
func ClaimLiveComment(ctx context.Context, tx pgx.Tx, env SendEnv, w order.Writer, a order.Actor, u identity.AuthUser,
	commentID uuid.UUID, in LiveClaimInput, now time.Time,
) (LiveClaimResult, error) {
	return live.Claim(ctx, tx, live.ClaimEnv{ //nolint:wrapcheck // sentinel của phần
		Send: env, Orders: w, Actor: a, Defaults: liveDefaults, Render: renderForOrder,
	}, u, commentID, in, now)
}

// renderForOrder điền mẫu templateID cho hội thoại conversationID với đơn orderID, người gửi actor (nil: tên Page)
// (tin xin địa chỉ, tin nhắc).
func renderForOrder(ctx context.Context, tx pgx.Tx, templateID, conversationID, orderID uuid.UUID, actor *identity.AuthUser,
) (string, error) {
	r, err := templates.Render(ctx, tx, templateID, templates.RenderInput{
		ConversationID: conversationID, OrderID: &orderID, Actor: actor,
	})
	return r.Text, err //nolint:wrapcheck // sentinel của mẫu
}

// liveDefaults đọc cài đặt Page accountID cho lần chốt: nguồn đơn của comment trên bài live bắt đầu lúc liveStart (còn
// bật) và mẫu xin địa chỉ mặc định.
func liveDefaults(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, liveStart time.Time) (live.PageDefaults, error) {
	s, err := readSettings(ctx, tx, accountID)
	if err != nil {
		return live.PageDefaults{}, err
	}
	src, err := activeSource(ctx, tx, autoSource(s, "comment", &liveStart))
	if err != nil {
		return live.PageDefaults{}, err
	}
	return live.PageDefaults{TrafficSourceID: src, AskTemplateID: s.AskAddressTemplateID}, nil
}

// GetLiveCommentContext trả bối cảnh chốt comment live commentID (cần conversation.read trên Page; không có →
// ErrNotFound).
func GetLiveCommentContext(ctx context.Context, tx pgx.Tx, u identity.AuthUser, commentID uuid.UUID) (LiveCommentContext, error) {
	c, err := live.Context(ctx, tx, u, commentID)
	if err != nil {
		return LiveCommentContext{}, err //nolint:wrapcheck // sentinel của phần
	}
	card, err := CustomerCard(ctx, tx, u, c.ConversationID)
	if err != nil {
		return LiveCommentContext{}, err
	}
	return LiveCommentContext{Card: card, OpenDrafts: c.OpenDrafts, Phones: c.Phones, SellerID: c.SellerID}, nil
}

// ListLiveComments trả comment của phiên live sessionID theo thứ tự đến và nextCursor (rỗng: hết) — cần
// conversation.read trên Page (không có → ErrNotFound).
func ListLiveComments(ctx context.Context, tx pgx.Tx, u identity.AuthUser, sessionID uuid.UUID, q LiveCommentQuery,
) ([]LiveCommentRow, string, error) {
	return live.ListComments(ctx, tx, u, sessionID, q) //nolint:wrapcheck // sentinel của phần
}
