package live

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Chốt đơn từ comment live (kế hoạch M4, Task 172; LV03, LV07, LV08, LV09; ADR-29, ADR-46 C11).

// Cách chốt (ClaimInput.Mode).
const (
	ModeNew        = "new"          // đơn nháp mới
	ModeAddToDraft = "add_to_draft" // thêm dòng vào đơn nháp đang có của khách trong phiên
)

// AskNotRequested là kết quả xin địa chỉ khi không gửi tin (không bấm, hay Page chưa có mẫu); AskQueued,
// AskSkippedPolicy ở send.
const AskNotRequested = "not_requested"

const (
	maxClaimLines  = 20
	maxClaimQty    = 20
	externalRefFmt = "fb_comment:%s"
	// noTemplateText là lý do khi bấm xin địa chỉ mà Page chưa chọn mẫu xin địa chỉ và người bấm không chọn mẫu.
	noTemplateText = "Page chưa chọn mẫu xin địa chỉ"
)

var (
	// ErrClaimInvalid là lỗi: yêu cầu chốt sai (cách chốt lạ, 1–20 dòng, số lượng 1–20; add_to_draft thiếu đơn hay
	// version) (400).
	ErrClaimInvalid = errors.New("live: yêu cầu chốt comment không hợp lệ")
	// ErrCommentClaimed là lỗi: comment đã có đơn (409 comment_claimed); chi tiết ở *CommentClaimedError.
	ErrCommentClaimed = errors.New("live: comment đã được chốt")
	// ErrOpenDraftExists là lỗi: khách đã có đơn nháp từ phiên này (409 open_draft_exists); chi tiết ở
	// *OpenDraftExistsError.
	ErrOpenDraftExists = errors.New("live: khách đã có đơn nháp trong phiên")
	// ErrSellerMismatch là lỗi: hàng không thuộc người bán của Page (422 seller_mismatch, ADR-29).
	ErrSellerMismatch = errors.New("live: hàng không thuộc người bán của Page")
	// ErrDraftNotOpen là lỗi: đơn chọn để thêm không còn là nháp (đã xác nhận, hủy, hết hạn) (409).
	ErrDraftNotOpen = errors.New("live: đơn không còn là nháp")
	// ErrDraftMismatch là lỗi: đơn chọn để thêm không phải của nhóm khách này hay không có ghi nhận trong phiên (422).
	ErrDraftMismatch = errors.New("live: đơn nháp không thuộc khách trong phiên")
)

// CommentClaimedError là chi tiết của ErrCommentClaimed: người đã chốt (tên hiển thị), đơn và mã đơn.
type CommentClaimedError struct {
	ByName    string
	OrderID   uuid.UUID
	OrderCode string
}

func (e *CommentClaimedError) Error() string {
	return "live: comment đã được chốt vào đơn " + e.OrderCode
}

// Is cho errors.Is(err, ErrCommentClaimed).
func (e *CommentClaimedError) Is(target error) bool { return target == ErrCommentClaimed }

// OpenDraftExistsError là chi tiết của ErrOpenDraftExists: các đơn nháp khách đang có trong phiên.
type OpenDraftExistsError struct {
	Drafts []Draft
}

func (e *OpenDraftExistsError) Error() string {
	return fmt.Sprintf("live: khách đã có %d đơn nháp trong phiên", len(e.Drafts))
}

// Is cho errors.Is(err, ErrOpenDraftExists).
func (e *OpenDraftExistsError) Is(target error) bool { return target == ErrOpenDraftExists }

// ClaimItem là một dòng chốt: biến thể và số lượng (1–20).
type ClaimItem struct {
	VariantID uuid.UUID
	Qty       int32
}

// ClaimInput là một lần bấm chốt comment.
type ClaimInput struct {
	Items           []ClaimItem
	Mode            string
	OrderID         *uuid.UUID
	ExpectedVersion int64
	ForceNew        bool
	AskAddress      bool
	TemplateID      *uuid.UUID
}

// PageDefaults là phần cài đặt Page dùng khi chốt: nguồn đơn theo khung giờ live (nil: không có hay đã tắt) và mẫu
// xin địa chỉ mặc định (nil: chưa chọn).
type PageDefaults struct {
	TrafficSourceID, AskTemplateID *uuid.UUID
}

// ClaimEnv là phụ thuộc của Claim: đường gửi tin, đường ghi đơn, người làm trên đơn, cài đặt Page (gói gốc đọc cài
// đặt và chọn nguồn đơn theo giờ bắt đầu phiên liveStart) và điền mẫu xin địa chỉ.
type ClaimEnv struct {
	Send     send.Env
	Orders   order.Writer
	Actor    order.Actor
	Defaults func(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, liveStart time.Time) (PageDefaults, error)
	// Render điền mẫu templateID cho hội thoại comment conversationID với đơn orderID, người gửi actor (nil: tên Page)
	// (templates.Render).
	Render func(ctx context.Context, tx pgx.Tx, templateID, conversationID, orderID uuid.UUID, actor *identity.AuthUser) (string, error)
}

// ClaimRef là dòng ghi nhận: người chốt (mã, tên hiển thị), đơn, mã đơn.
type ClaimRef struct {
	By        uuid.UUID
	ByName    string
	OrderID   uuid.UUID
	OrderCode string
}

// Ask là kết quả xin địa chỉ: queued, skipped_policy (Reason là câu tiếng Việt), not_requested.
type Ask struct {
	Status, Reason string
	MessageID      *uuid.UUID
}

// ClaimResult là kết quả Claim: đơn sau khi chốt; Created khi là đơn mới (Replayed khi khóa nghiệp vụ trả đơn cũ);
// Before, Fields là đơn trước khi sửa và các trường đổi khi thêm vào nháp (bên gọi ghi audit).
type ClaimResult struct {
	Order            order.Order
	Created          bool
	Replayed         bool
	Before           order.Order
	Fields           []string
	Claim            ClaimRef
	Ask              Ask
	ConversationID   uuid.UUID
	ChannelAccountID uuid.UUID
}

func (in ClaimInput) validate() error {
	if len(in.Items) == 0 || len(in.Items) > maxClaimLines {
		return ErrClaimInvalid
	}
	for _, it := range in.Items {
		if it.VariantID == uuid.Nil || it.Qty < 1 || it.Qty > maxClaimQty {
			return ErrClaimInvalid
		}
	}
	switch in.Mode {
	case ModeNew:
		return nil
	case ModeAddToDraft:
		if in.OrderID == nil || in.ExpectedVersion < 1 {
			return ErrClaimInvalid
		}
		return nil
	}
	return ErrClaimInvalid
}

// claimRun là trạng thái một lần Claim (dựng mới mỗi lần chạy, không giữ gì giữa các lần RetryTx).
type claimRun struct {
	env     ClaimEnv
	tx      pgx.Tx
	q       *store.Queries
	u       identity.AuthUser
	in      ClaimInput
	now     time.Time
	cm      store.GetLiveCommentRow
	session Session
	acc     channel.ChannelAccount
	root    uuid.UUID
	inbox   uuid.UUID // hộp thư của khách đã khóa ở bước 1 (uuid.Nil: chưa có)
}

// Claim chốt comment live commentID bằng nhân viên u lúc now, trong tx tenant của bên gọi (WriteTx). Mọi lỗi → bên gọi
// lùi cả transaction (không đơn, không giữ hàng, không ghi nhận, không tin). Thứ tự khóa (ADR-46, C11 khách trước hội
// thoại; cùng thứ tự với đường tin vào):
//
//  0. customer.LockRoot(khách của comment) — nhóm 5.
//  1. Hội thoại comment và hộp thư của cùng khách trên Page, ORDER BY id FOR UPDATE — nhóm 6 (hộp thư chưa có thì
//     tạo lúc xin địa chỉ: dòng mới).
//  2. Đã có ghi nhận cho comment → *CommentClaimedError.
//  3. ModeNew, khách (theo gốc) có đơn nháp từ phiên mà không ForceNew → *OpenDraftExistsError.
//  4. ModeNew: order.Create (đơn nháp, giữ hàng; nhóm 7, 11), người bán của Page (hàng khác người bán →
//     ErrSellerMismatch). ModeAddToDraft: order.GetDetail rồi order.Edit với dòng cũ + dòng mới (cộng số lượng khi
//     trùng biến thể). Lỗi của order (hết hàng, ngừng bán, version) trả nguyên (đã bọc).
//  5. Xin địa chỉ (send.AskAddress): mẫu chọn hay mẫu mặc định của Page; chính sách gửi quyết nhắn riêng, tin thường
//     hay bỏ (tin, lệnh là dòng mới).
//  6. Chèn ghi nhận kèm tin xin địa chỉ (đã có → *CommentClaimedError).
//  7. Báo hiệu live.comments, conversation.changed.
//
// Lỗi khác: comment không có, không phải comment live của khách, Page không có live.operate → ErrNotFound.
func Claim(ctx context.Context, tx pgx.Tx, env ClaimEnv, u identity.AuthUser, commentID uuid.UUID, in ClaimInput,
	now time.Time,
) (ClaimResult, error) {
	if err := in.validate(); err != nil {
		return ClaimResult{}, err
	}
	r := &claimRun{env: env, tx: tx, q: store.New(tx), u: u, in: in, now: now.UTC()}
	if err := r.load(ctx, commentID); err != nil {
		return ClaimResult{}, err
	}
	if err := r.lock(ctx); err != nil {
		return ClaimResult{}, err
	}
	def, err := env.Defaults(ctx, tx, r.acc.ID, r.session.StartedAt)
	if err != nil {
		return ClaimResult{}, err
	}
	var res ClaimResult
	if in.Mode == ModeNew {
		res, err = r.create(ctx, def)
	} else {
		res, err = r.addToDraft(ctx)
	}
	if err != nil {
		return ClaimResult{}, err
	}
	if res.Ask, err = r.ask(ctx, def, res.Order); err != nil {
		return ClaimResult{}, err
	}
	if res.Claim, err = r.record(ctx, res.Order, res.Ask); err != nil {
		return ClaimResult{}, err
	}
	res.ConversationID, res.ChannelAccountID = r.cm.ConversationID, r.acc.ID
	return res, r.signal(ctx)
}

// load đọc comment, kiểm quyền live.operate trên Page, phiên live của bài, Page.
func (r *claimRun) load(ctx context.Context, commentID uuid.UUID) error {
	cm, err := r.q.GetLiveComment(ctx, commentID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return fmt.Errorf("đọc comment live: %w", err)
	}
	if err := access.Require(r.u, permOperate, cm.ChannelAccountID); err != nil {
		return err //nolint:wrapcheck // sentinel của phần (404)
	}
	if !cm.IsLive || cm.Direction != "in" || cm.Origin != "customer" || !cm.ExternalID.Valid {
		return ErrNotFound
	}
	sid, err := IsLiveComment(ctx, r.tx, cm.ChannelAccountID, cm.PostExternalID)
	if err != nil {
		return err
	}
	if sid == nil {
		return ErrNotFound
	}
	row, err := r.q.GetLiveSession(ctx, *sid)
	if err != nil {
		return fmt.Errorf("đọc phiên live của comment: %w", err)
	}
	acc, err := channel.Account(ctx, r.tx, cm.ChannelAccountID)
	if err != nil {
		return err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	r.cm, r.session, r.acc = cm, sessionFrom(row), acc
	return nil
}

// lock là bước 0–2: khóa khách gốc, hai hội thoại theo id tăng dần, rồi kiểm comment chưa chốt.
func (r *claimRun) lock(ctx context.Context) error {
	root, err := customer.LockRoot(ctx, r.tx, r.cm.CustomerID)
	if err != nil {
		return fmt.Errorf("khóa khách của comment: %w", err)
	}
	r.root = root
	ids := []uuid.UUID{r.cm.ConversationID}
	inbox, err := send.InboxFor(ctx, r.tx, r.cm.ConversationID)
	if err != nil {
		return err //nolint:wrapcheck // send đã bọc việc đang làm
	}
	if inbox != uuid.Nil {
		ids = append(ids, inbox)
	}
	r.inbox = inbox
	if _, err := r.q.LockConversationsByID(ctx, ids); err != nil {
		return fmt.Errorf("khóa hội thoại của comment: %w", err)
	}
	return r.notClaimed(ctx)
}

// notClaimed trả *CommentClaimedError khi comment đã có dòng ghi nhận.
func (r *claimRun) notClaimed(ctx context.Context) error {
	c, err := r.q.GetCommentClaim(ctx, r.cm.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("đọc ghi nhận của comment: %w", err)
	}
	return &CommentClaimedError{ByName: c.ClaimedByName, OrderID: c.OrderID, OrderCode: c.OrderCode}
}

// create là bước 3–4 của ModeNew.
func (r *claimRun) create(ctx context.Context, def PageDefaults) (ClaimResult, error) {
	if !r.in.ForceNew {
		drafts, err := openDrafts(ctx, r.q, r.root, r.session.ID)
		if err != nil {
			return ClaimResult{}, err
		}
		if len(drafts) > 0 {
			return ClaimResult{}, &OpenDraftExistsError{Drafts: drafts}
		}
	}
	lines := make([]order.LineInput, 0, len(r.in.Items))
	for _, it := range r.in.Items {
		lines = append(lines, order.LineInput{VariantID: it.VariantID, Quantity: it.Qty})
	}
	conv, acc, cust := r.cm.ConversationID, r.acc.ID, r.cm.CustomerID
	res, err := r.env.Orders.Create(ctx, r.tx, order.CreateInput{
		IdempotencyKey: uuid.NewSHA1(r.acc.TenantID, r.cm.ID[:]), SellerID: r.acc.SellerID, Channel: order.ChannelFacebook,
		ExternalRef: fmt.Sprintf(externalRefFmt, r.cm.ExternalID.String), TrafficSourceID: def.TrafficSourceID,
		Customer: customer.OrderCustomerInput{CustomerID: &cust, Source: customer.SourceChat}, Lines: lines,
		Intent: order.IntentDraft, ConversationID: &conv, ChannelAccountID: &acc,
	}, r.env.Actor, r.now)
	if err != nil {
		return ClaimResult{}, sellerErr(fmt.Errorf("tạo đơn nháp từ comment: %w", err))
	}
	if res.Order.SellerID != r.acc.SellerID {
		return ClaimResult{}, ErrSellerMismatch
	}
	return ClaimResult{Order: res.Order, Created: true, Replayed: res.Replayed}, nil
}

// addToDraft là bước 4 của ModeAddToDraft: đơn phải còn nháp, của nhóm khách, có ghi nhận trong phiên.
func (r *claimRun) addToDraft(ctx context.Context) (ClaimResult, error) {
	d, err := order.GetDetail(ctx, r.tx, r.acc.SellerID, *r.in.OrderID)
	if errors.Is(err, order.ErrNotFound) {
		return ClaimResult{}, ErrNotFound
	}
	if err != nil {
		return ClaimResult{}, fmt.Errorf("đọc đơn nháp để thêm: %w", err)
	}
	if d.Status != order.StatusDraft {
		return ClaimResult{}, ErrDraftNotOpen
	}
	inSession, err := r.q.OrderClaimedInSession(ctx, store.OrderClaimedInSessionParams{OrderID: d.ID, LiveSessionID: r.session.ID})
	if err != nil {
		return ClaimResult{}, fmt.Errorf("đọc ghi nhận của đơn nháp: %w", err)
	}
	if d.Customer.RootID != r.root || !inSession {
		return ClaimResult{}, ErrDraftMismatch
	}
	lines := mergeLines(d.Items, r.in.Items)
	res, err := r.env.Orders.Edit(ctx, r.tx, r.acc.SellerID, d.ID, order.EditInput{Version: r.in.ExpectedVersion, Lines: &lines},
		r.env.Actor, r.now)
	if err != nil {
		return ClaimResult{}, sellerErr(fmt.Errorf("thêm vào đơn nháp: %w", err))
	}
	return ClaimResult{Order: res.Order, Before: d.Order, Fields: res.Fields}, nil
}

// sellerErr: biến thể không thuộc người bán của Page (order.ErrMixedSellers, vì đơn tạo theo người bán của Page) là
// ErrSellerMismatch (ADR-29); lỗi khác trả nguyên.
func sellerErr(err error) error {
	if errors.Is(err, order.ErrMixedSellers) {
		return ErrSellerMismatch // không bọc lỗi của order: mã HTTP là seller_mismatch, không phải mixed_sellers
	}
	return err
}

// mergeLines là dòng mong muốn của đơn sau khi thêm items: dòng cũ giữ ItemID (cộng số lượng khi trùng biến thể), biến
// thể mới thành dòng mới theo thứ tự gửi.
func mergeLines(cur []order.Item, items []ClaimItem) []order.EditLine {
	out := make([]order.EditLine, 0, len(cur)+len(items))
	at := map[uuid.UUID]int{}
	for _, it := range cur {
		id := it.ID
		at[it.VariantID] = len(out)
		out = append(out, order.EditLine{ItemID: &id, VariantID: it.VariantID, Quantity: it.Qty, LineDiscount: it.LineDiscount})
	}
	for _, it := range items {
		if i, ok := at[it.VariantID]; ok {
			out[i].Quantity += it.Qty
			continue
		}
		at[it.VariantID] = len(out)
		out = append(out, order.EditLine{VariantID: it.VariantID, Quantity: it.Qty})
	}
	return out
}

// ask là bước 5: không bấm xin địa chỉ → not_requested; mẫu chọn hay mẫu mặc định (không có → not_requested kèm lý do);
// điền mẫu với đơn o rồi send.AskAddress.
func (r *claimRun) ask(ctx context.Context, def PageDefaults, o order.Order) (Ask, error) {
	if !r.in.AskAddress {
		return Ask{Status: AskNotRequested}, nil
	}
	tpl := r.in.TemplateID
	if tpl == nil {
		tpl = def.AskTemplateID
	}
	if tpl == nil {
		return Ask{Status: AskNotRequested, Reason: noTemplateText}, nil
	}
	// Người gửi {nhan_vien} là người bấm khi họ được đọc hội thoại của Page (đường chốt chỉ đòi live.operate); không thì
	// tên Page.
	var actor *identity.AuthUser
	if identity.Can(r.u, permRead, access.PageScope(r.acc.ID)) {
		actor = &r.u
	}
	text, err := r.env.Render(ctx, r.tx, *tpl, r.cm.ConversationID, o.ID, actor)
	if err != nil {
		return Ask{}, err
	}
	a, err := send.AskAddress(ctx, r.tx, r.env.Send, r.u, r.cm.ID, r.inbox, text, r.now)
	if err != nil {
		return Ask{}, err //nolint:wrapcheck // send đã bọc việc đang làm
	}
	out := Ask{Status: a.Status, Reason: a.Reason}
	if a.Message != nil {
		id := a.Message.ID
		out.MessageID = &id
	}
	return out, nil
}

// record là bước 6: chèn ghi nhận (đã có → *CommentClaimedError) và trả dòng ghi nhận.
func (r *claimRun) record(ctx context.Context, o order.Order, a Ask) (ClaimRef, error) {
	arg := store.InsertCommentClaimParams{
		MessageID: r.cm.ID, LiveSessionID: r.session.ID, ConversationID: r.cm.ConversationID, OrderID: o.ID,
		ClaimedBy: r.u.UserID, ClaimedAt: r.now, AskStatus: a.Status, AskReason: pgtype.Text{String: a.Reason, Valid: a.Reason != ""},
	}
	if a.MessageID != nil {
		arg.AskMessageID = pgtype.UUID{Bytes: *a.MessageID, Valid: true}
	}
	n, err := r.q.InsertCommentClaim(ctx, arg)
	if err != nil {
		return ClaimRef{}, fmt.Errorf("ghi nhận chốt comment: %w", err)
	}
	if n == 0 {
		if err := r.notClaimed(ctx); err != nil {
			return ClaimRef{}, err
		}
		return ClaimRef{}, ErrCommentClaimed
	}
	name, err := r.q.TemplateStaffName(ctx, r.u.UserID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ClaimRef{}, fmt.Errorf("đọc tên người chốt: %w", err)
	}
	return ClaimRef{By: r.u.UserID, ByName: name, OrderID: o.ID, OrderCode: o.Code}, nil
}

// signal là bước 7: live.comments của phiên, conversation.changed của hội thoại comment.
func (r *claimRun) signal(ctx context.Context) error {
	bus := r.env.Send.Bus
	if bus == nil {
		return nil
	}
	if err := OnComment(ctx, r.tx, bus, r.acc.TenantID, r.acc.ID, r.session.ID); err != nil {
		return err
	}
	if err := bus.Publish(ctx, r.tx, realtime.Signal{
		TenantID: r.acc.TenantID, Topic: deps.TopicConversationChanged, Requires: permRead,
		Scope: access.PageScope(r.acc.ID), Entity: realtime.Entity{Type: deps.EntityConversation, ID: r.cm.ConversationID.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu hội thoại đổi: %w", err)
	}
	return nil
}
