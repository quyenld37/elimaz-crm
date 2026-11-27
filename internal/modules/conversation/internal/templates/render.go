package templates

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/queries"
)

// Biến của mẫu (kế hoạch M4, Task 161, 162; 01 mục 6). Biến là "{tên}" với tên gồm chữ thường a–z và "_"; chuỗi trong
// ngoặc nhọn khác (có dấu cách, chữ hoa, có dấu, rỗng) là chữ thường, giữ nguyên.
const (
	VarCustomerName = "ten_khach"  // tên khách (tên tạm → "bạn")
	VarPageName     = "ten_page"   // tên Page của hội thoại
	VarOrderCode    = "ma_don"     // mã đơn được chỉ định, hoặc đơn chưa hủy mới nhất của nhóm khách
	VarStaffName    = "nhan_vien"  // tên hiển thị của người gửi; tin tự động → tên Page
	VarTrackingCode = "ma_van_don" // mã vận đơn hiện hành của đơn (Task 162; chưa có → thiếu giá trị)
)

// placeholderAddress là cách gọi khách khi khách chỉ có tên tạm (name_source placeholder).
const placeholderAddress = "bạn"

// varPattern khớp một biến {tên}.
var varPattern = regexp.MustCompile(`\{([a-z_]+)\}`)

func knownVariable(name string) bool {
	return slices.Contains([]string{VarCustomerName, VarPageName, VarOrderCode, VarStaffName, VarTrackingCode}, name)
}

var (
	// ErrUnknownVariable là lỗi khi mẫu có biến {tên} không nằm trong danh sách biến (422 lúc lưu).
	ErrUnknownVariable = errors.New("conversation: mẫu có biến không biết")
	// ErrOrderNotOfCustomer là lỗi khi đơn chỉ định cho {ma_don} không có hay không thuộc nhóm khách của hội thoại (422).
	ErrOrderNotOfCustomer = errors.New("conversation: đơn không phải của khách trong hội thoại")
)

// VariableMissingError là lỗi khi một biến của mẫu không có giá trị cho hội thoại này (API 422
// template_variable_missing kèm name).
type VariableMissingError struct {
	Name string
}

func (e *VariableMissingError) Error() string {
	return "conversation: biến " + e.Name + " của mẫu không có giá trị"
}

// Variables trả tên các biến đã biết có trong s (theo thứ tự gặp lần đầu, không trùng); có biến lạ → ErrUnknownVariable.
func Variables(s string) ([]string, error) {
	var out []string
	for _, m := range varPattern.FindAllStringSubmatch(s, -1) {
		if !knownVariable(m[1]) {
			return nil, fmt.Errorf("%w: {%s}", ErrUnknownVariable, m[1])
		}
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out, nil
}

// Fill thay mỗi biến đã biết {tên} trong s bằng value(tên) (lỗi của value trả nguyên); biến lạ → ErrUnknownVariable.
// Giá trị điền vào không bị quét lại.
func Fill(s string, value func(name string) (string, error)) (string, error) {
	var firstErr error
	out := varPattern.ReplaceAllStringFunc(s, func(m string) string {
		if firstErr != nil {
			return m
		}
		name := m[1 : len(m)-1]
		if !knownVariable(name) {
			firstErr = fmt.Errorf("%w: {%s}", ErrUnknownVariable, name)
			return m
		}
		v, err := value(name)
		if err != nil {
			firstErr = err
			return m
		}
		return v
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// RenderInput là hội thoại cần điền mẫu, đơn chỉ định cho {ma_don} (nil = đơn chưa hủy mới nhất của nhóm khách) và người
// gửi (nil = tin tự động: không kiểm quyền, {nhan_vien} là tên Page).
type RenderInput struct {
	ConversationID uuid.UUID
	OrderID        *uuid.UUID
	Actor          *identity.AuthUser
}

// Rendered là mẫu đã điền biến (thân và nhãn nút trả lời nhanh), kèm tệp đính kèm của mẫu.
type Rendered struct {
	Text             string
	AttachmentFileID *uuid.UUID
	QuickReplies     []channel.QuickReply
}

// Render điền biến của mẫu templateID cho hội thoại in.ConversationID, trong tx tenant của bên gọi. Dùng chung cho
// màn hình (POST /reply-templates/{id}/render), quy tắc tự động và Live (Task 165–173). Lỗi: hội thoại, mẫu không có,
// mẫu riêng của Page khác, Actor không có conversation.read trên Page của hội thoại → ErrNotFound; đơn chỉ định không
// thuộc nhóm khách → ErrOrderNotOfCustomer; biến không có giá trị → *VariableMissingError.
func Render(ctx context.Context, tx pgx.Tx, templateID uuid.UUID, in RenderInput) (Rendered, error) {
	q := store.New(tx)
	conv, err := q.ReminderConversation(ctx, in.ConversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Rendered{}, ErrNotFound
	}
	if err != nil {
		return Rendered{}, fmt.Errorf("đọc hội thoại của mẫu: %w", err)
	}
	if in.Actor != nil {
		if err := access.Require(*in.Actor, permRead, conv.ChannelAccountID); err != nil {
			return Rendered{}, err //nolint:wrapcheck // sentinel của access
		}
	}
	row, err := q.GetReplyTemplate(ctx, templateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Rendered{}, ErrNotFound
	}
	if err != nil {
		return Rendered{}, fmt.Errorf("đọc mẫu trả lời: %w", err)
	}
	t, err := fromRow(row)
	if err != nil {
		return Rendered{}, err
	}
	if t.PageID != nil && *t.PageID != conv.ChannelAccountID {
		return Rendered{}, ErrNotFound
	}
	v := &values{tx: tx, q: q, conv: conv, in: in, cache: map[string]string{}}
	get := func(name string) (string, error) { return v.get(ctx, name) }
	out := Rendered{AttachmentFileID: t.AttachmentFileID, QuickReplies: make([]channel.QuickReply, 0, len(t.QuickReplies))}
	if out.Text, err = Fill(t.Body, get); err != nil {
		return Rendered{}, err
	}
	for _, qr := range t.QuickReplies {
		title, err := Fill(qr.Title, get)
		if err != nil {
			return Rendered{}, err
		}
		out.QuickReplies = append(out.QuickReplies, channel.QuickReply{Title: title, Payload: qr.Payload})
	}
	return out, nil
}

// values tính giá trị biến cho một lần Render, mỗi biến đọc database một lần.
type values struct {
	tx    pgx.Tx
	q     *store.Queries
	conv  store.ReminderConversationRow
	in    RenderInput
	cache map[string]string
}

func (v *values) get(ctx context.Context, name string) (string, error) {
	if s, ok := v.cache[name]; ok {
		return s, nil
	}
	var (
		s   string
		err error
	)
	switch name {
	case VarCustomerName:
		s, err = v.customerName(ctx)
	case VarPageName:
		s, err = v.pageName(ctx)
	case VarOrderCode:
		s, err = v.orderCode(ctx)
	case VarStaffName:
		s, err = v.staffName(ctx)
	case VarTrackingCode:
		s, err = v.trackingCode(ctx)
	}
	if err != nil {
		return "", err
	}
	if s == "" {
		return "", &VariableMissingError{Name: name}
	}
	v.cache[name] = s
	return s, nil
}

func (v *values) customerName(ctx context.Context) (string, error) {
	r, err := v.q.TemplateCustomerName(ctx, v.conv.CustomerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", &VariableMissingError{Name: VarCustomerName}
	}
	if err != nil {
		return "", fmt.Errorf("đọc tên khách cho mẫu: %w", err)
	}
	if r.NameSource == customer.NameSourcePlaceholder {
		return placeholderAddress, nil
	}
	return r.FullName, nil
}

func (v *values) pageName(ctx context.Context) (string, error) {
	a, err := channel.Account(ctx, v.tx, v.conv.ChannelAccountID)
	if errors.Is(err, channel.ErrAccountNotFound) {
		return "", &VariableMissingError{Name: VarPageName}
	}
	if err != nil {
		return "", fmt.Errorf("đọc Page cho mẫu: %w", err)
	}
	return a.Name, nil
}

func (v *values) staffName(ctx context.Context) (string, error) {
	if v.in.Actor == nil {
		return v.get(ctx, VarPageName)
	}
	s, err := v.q.TemplateStaffName(ctx, v.in.Actor.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", &VariableMissingError{Name: VarStaffName}
	}
	if err != nil {
		return "", fmt.Errorf("đọc tên nhân viên cho mẫu: %w", err)
	}
	return s, nil
}

// orderCode là mã của đơn dùng cho {ma_don}.
func (v *values) orderCode(ctx context.Context) (string, error) {
	_, code, err := v.order(ctx)
	return code, err
}

// trackingCode là mã vận đơn của vận đơn hiện hành của đơn dùng cho {ma_don} (cùng đơn, Task 162). Khách chưa có đơn,
// đơn chưa có vận đơn hiện hành hay vận đơn chưa có mã → thiếu giá trị của chính biến này.
func (v *values) trackingCode(ctx context.Context) (string, error) {
	id, _, err := v.order(ctx)
	if _, ok := errors.AsType[*VariableMissingError](err); ok {
		return "", &VariableMissingError{Name: VarTrackingCode}
	}
	if err != nil {
		return "", err
	}
	code, ok, err := queries.TrackingCodeOfOrder(ctx, v.tx, id)
	if err != nil {
		return "", fmt.Errorf("đọc mã vận đơn cho mẫu: %w", err)
	}
	if !ok {
		return "", &VariableMissingError{Name: VarTrackingCode}
	}
	return code, nil
}

// order chọn đơn cho {ma_don} và {ma_van_don}: đơn chỉ định (phải thuộc nhóm khách) hoặc đơn chưa hủy mới nhất của
// nhóm khách. Hai biến cùng đọc trong một tx của bên gọi nên cùng một đơn.
func (v *values) order(ctx context.Context) (uuid.UUID, string, error) {
	root, err := customer.RootOf(ctx, v.tx, v.conv.CustomerID)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("đọc gốc khách cho mẫu: %w", err)
	}
	if v.in.OrderID != nil {
		o, err := v.q.TemplateOrder(ctx, *v.in.OrderID)
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, "", ErrOrderNotOfCustomer
		}
		if err != nil {
			return uuid.Nil, "", fmt.Errorf("đọc đơn cho mẫu: %w", err)
		}
		owner, err := customer.RootOf(ctx, v.tx, o.CustomerID)
		if err != nil && !errors.Is(err, customer.ErrNotFound) {
			return uuid.Nil, "", fmt.Errorf("đọc khách của đơn cho mẫu: %w", err)
		}
		if err != nil || owner != root {
			return uuid.Nil, "", ErrOrderNotOfCustomer
		}
		return *v.in.OrderID, o.Code, nil
	}
	members, err := customer.GroupMembers(ctx, v.tx, root)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("đọc nhóm khách cho mẫu: %w", err)
	}
	o, err := v.q.TemplateLatestOrder(ctx, members)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", &VariableMissingError{Name: VarOrderCode}
	}
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("đọc đơn mới nhất cho mẫu: %w", err)
	}
	return o.ID, o.Code, nil
}
