package conversation

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/templates"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Mẫu trả lời có biến và phím tắt "/" (Task 161; 01 mục 6). Hiện thực ở internal/templates. Mẫu chung chỉ người có
// conversation.settings phạm vi all tạo, sửa, xóa; mẫu của một Page cần quyền đó trên Page.

// ReplyTemplate là một mẫu trả lời (PageID nil = mẫu chung).
type ReplyTemplate = templates.Template

// TemplateInput là dữ liệu tạo mẫu.
type TemplateInput = templates.Input

// TemplatePatch là phần sửa của mẫu (chỉ trường được gửi).
type TemplatePatch = templates.Patch

// TemplateWriter là đường ghi mẫu (quyền, version, audit trong tx của bên gọi).
type TemplateWriter = templates.Writer

// RenderInput là hội thoại, đơn chỉ định cho {ma_don} và người gửi (nil = tin tự động) khi điền mẫu.
type RenderInput = templates.RenderInput

// Rendered là mẫu đã điền biến.
type Rendered = templates.Rendered

// VariableMissingError là lỗi: biến Name của mẫu không có giá trị cho hội thoại (422 template_variable_missing).
type VariableMissingError = templates.VariableMissingError

// Lỗi của mẫu trả lời (mẫu không có, hội thoại không đọc được: ErrNotFound).
var (
	// ErrShortcutTaken là lỗi: phím tắt đã có ở mẫu còn sống khác (409).
	ErrShortcutTaken = templates.ErrShortcutTaken
	// ErrTemplateInvalid là lỗi: dữ liệu mẫu sai, tệp hay Page không có (422).
	ErrTemplateInvalid = templates.ErrInvalid
	// ErrUnknownVariable là lỗi: mẫu có biến {tên} không nằm trong danh sách biến (422).
	ErrUnknownVariable = templates.ErrUnknownVariable
	// ErrTemplateForbidden là lỗi: không có conversation.settings đủ phạm vi cho mẫu này (403).
	ErrTemplateForbidden = templates.ErrForbidden
	// ErrTemplateVersionConflict là lỗi: mẫu đã đổi từ lúc tải (409).
	ErrTemplateVersionConflict = templates.ErrVersionConflict
	// ErrOrderNotOfCustomer là lỗi: đơn chỉ định không thuộc nhóm khách của hội thoại (422).
	ErrOrderNotOfCustomer = templates.ErrOrderNotOfCustomer
)

// ListTemplates trả mẫu còn sống khớp tiền tố q (phím tắt hay tiêu đề) cho Page pageID (nil = mọi Page u đọc được),
// kèm mẫu chung (xem templates.List).
func ListTemplates(ctx context.Context, tx pgx.Tx, u identity.AuthUser, q string, pageID *uuid.UUID) ([]ReplyTemplate, error) {
	return templates.List(ctx, tx, u, q, pageID) //nolint:wrapcheck // sentinel của phần
}

// RenderTemplate điền biến của mẫu templateID cho hội thoại in.ConversationID (xem templates.Render); dùng chung cho
// màn hình, quy tắc tự động và Live.
func RenderTemplate(ctx context.Context, tx pgx.Tx, templateID uuid.UUID, in RenderInput) (Rendered, error) {
	return templates.Render(ctx, tx, templateID, in) //nolint:wrapcheck // sentinel của phần
}
