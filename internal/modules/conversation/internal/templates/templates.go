// Package templates là phần của module conversation: mẫu trả lời có biến và phím tắt "/" (Task 161, 162; 01 mục 6).
// Mẫu là tài nguyên của shop: mẫu chung (không Page) chỉ người có conversation.settings phạm vi all tạo, sửa, xóa
// (quyết định chủ shop 26/11); mẫu riêng một Page cần conversation.settings trên Page đó. Thân mẫu là chữ shop soạn;
// mẫu đã điền tên khách (Render) là dữ liệu cá nhân, không vào log, audit.
package templates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
)

// Parts là phần chạy nền của phần templates: không có (mẫu chỉ có đường API).
func Parts(_ deps.Deps) deps.Parts { return deps.Parts{} }

// Giới hạn của mẫu (khớp CHECK của 0405 và hợp đồng CreateReplyTemplateRequest, QuickReply).
const (
	maxTitleRunes   = 100
	maxBodyRunes    = 2000
	maxQuickReplies = 13
	maxQRTitleRunes = 20
	maxQRPayload    = 1000
	listLimit       = 200
	// EntityTemplate là loại thực thể trong audit.
	EntityTemplate = "reply_template"
)

// Quyền dùng ở phần này (phạm vi page:<channel_accounts.id>).
const (
	permRead     identity.Permission = "conversation.read"
	permSettings identity.Permission = "conversation.settings"
)

// pgUniqueViolation, pgForeignKeyViolation: mã lỗi PostgreSQL khi trùng phím tắt, tệp/Page không có.
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

var shortcutPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

var (
	// ErrNotFound là lỗi khi mẫu (hay hội thoại, Page) không có, đã xóa, hay người gọi không đọc được Page (404).
	ErrNotFound = access.ErrNotFound
	// ErrShortcutTaken là lỗi khi phím tắt (không phân biệt hoa thường) đã có ở mẫu còn sống khác của shop (409).
	ErrShortcutTaken = errors.New("conversation: phím tắt mẫu trả lời đã có")
	// ErrInvalid là lỗi khi dữ liệu mẫu sai: phím tắt, tiêu đề, thân, nút, tệp hay Page không có (422).
	ErrInvalid = errors.New("conversation: mẫu trả lời không hợp lệ")
	// ErrForbidden là lỗi khi người gọi không có conversation.settings đủ phạm vi: mẫu chung cần phạm vi all, mẫu của
	// Page cần quyền trên Page đó (403).
	ErrForbidden = errors.New("conversation: không có quyền sửa mẫu trả lời này")
	// ErrVersionConflict là lỗi khi expectedVersion khác version hiện tại (409).
	ErrVersionConflict = errors.New("conversation: mẫu trả lời đã đổi, tải lại rồi thử lại")
)

// Template là một mẫu trả lời. PageID nil = mẫu chung.
type Template struct {
	ID               uuid.UUID
	PageID           *uuid.UUID
	Shortcut, Title  string
	Body             string
	AttachmentFileID *uuid.UUID
	QuickReplies     []channel.QuickReply
	Version          int32
	CreatedAt        time.Time
}

// Input là dữ liệu tạo mẫu.
type Input struct {
	PageID           *uuid.UUID
	Shortcut, Title  string
	Body             string
	AttachmentFileID *uuid.UUID
	QuickReplies     []channel.QuickReply
}

// Patch là phần sửa của mẫu: chỉ trường khác nil (hay Set* = true) được đổi. SetPage với PageID nil = chuyển thành mẫu
// chung; SetAttachment với AttachmentFileID nil = bỏ tệp.
type Patch struct {
	SetPage          bool
	PageID           *uuid.UUID
	Shortcut, Title  *string
	Body             *string
	SetAttachment    bool
	AttachmentFileID *uuid.UUID
	QuickReplies     *[]channel.QuickReply
	ExpectedVersion  int32
}

// Writer là đường ghi mẫu của nhân viên: kiểm quyền theo phạm vi, khóa dòng rồi so version, audit (RequestID của yêu
// cầu) trong tx của bên gọi.
type Writer struct {
	RequestID string
}

// Create tạo mẫu in của u. Lỗi: ErrForbidden, ErrInvalid, ErrUnknownVariable, ErrShortcutTaken.
func (w Writer) Create(ctx context.Context, tx pgx.Tx, u identity.AuthUser, in Input) (Template, error) {
	in.Shortcut = strings.ToLower(strings.TrimSpace(in.Shortcut))
	in.Title = strings.TrimSpace(in.Title)
	if err := checkManage(ctx, tx, u, in.PageID); err != nil {
		return Template{}, err
	}
	qr, err := prepare(in)
	if err != nil {
		return Template{}, err
	}
	row, err := store.New(tx).InsertReplyTemplate(ctx, store.InsertReplyTemplateParams{
		ChannelAccountID: db.NullUUID(in.PageID), Shortcut: in.Shortcut, Title: in.Title, Body: in.Body,
		AttachmentFileID: db.NullUUID(in.AttachmentFileID), QuickReplies: qr, CreatedBy: pgtype.UUID{Bytes: u.UserID, Valid: true},
	})
	if err != nil {
		return Template{}, writeErr(err, "tạo mẫu trả lời")
	}
	t, err := fromRow(store.GetReplyTemplateRow(row))
	if err != nil {
		return Template{}, err
	}
	return t, w.audit(ctx, tx, u, "reply_template.create", t.ID, nil, auditView(t))
}

// Update sửa mẫu id theo p khi p.ExpectedVersion khớp; version tăng 1. Người gọi cần quyền quản lý ở cả phạm vi cũ và
// mới của mẫu. Lỗi: ErrNotFound, ErrForbidden, ErrVersionConflict, ErrInvalid, ErrUnknownVariable, ErrShortcutTaken.
func (w Writer) Update(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID, p Patch) (Template, error) {
	q := store.New(tx)
	cur, err := lockTemplate(ctx, q, id)
	if err != nil {
		return Template{}, err
	}
	if err := checkManage(ctx, tx, u, cur.PageID); err != nil {
		return Template{}, err
	}
	if cur.Version != p.ExpectedVersion {
		return Template{}, ErrVersionConflict
	}
	next := apply(cur, p)
	if p.SetPage {
		if err := checkManage(ctx, tx, u, next.PageID); err != nil {
			return Template{}, err
		}
	}
	in := Input{
		PageID: next.PageID, Shortcut: next.Shortcut, Title: next.Title, Body: next.Body,
		AttachmentFileID: next.AttachmentFileID, QuickReplies: next.QuickReplies,
	}
	qr, err := prepare(in)
	if err != nil {
		return Template{}, err
	}
	row, err := q.UpdateReplyTemplate(ctx, store.UpdateReplyTemplateParams{
		ID: id, ChannelAccountID: db.NullUUID(in.PageID), Shortcut: in.Shortcut, Title: in.Title, Body: in.Body,
		AttachmentFileID: db.NullUUID(in.AttachmentFileID), QuickReplies: qr,
	})
	if err != nil {
		return Template{}, writeErr(err, "sửa mẫu trả lời")
	}
	t, err := fromRow(store.GetReplyTemplateRow(row))
	if err != nil {
		return Template{}, err
	}
	return t, w.audit(ctx, tx, u, "reply_template.update", id, auditView(cur), auditView(t))
}

// Delete xóa mềm mẫu id (phím tắt dùng lại được). Lỗi: ErrNotFound (kể cả đã xóa), ErrForbidden.
func (w Writer) Delete(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID) error {
	q := store.New(tx)
	cur, err := lockTemplate(ctx, q, id)
	if err != nil {
		return err
	}
	if err := checkManage(ctx, tx, u, cur.PageID); err != nil {
		return err
	}
	if _, err := q.SoftDeleteReplyTemplate(ctx, id); err != nil {
		return fmt.Errorf("xóa mẫu trả lời: %w", err)
	}
	return w.audit(ctx, tx, u, "reply_template.delete", id, auditView(cur), map[string]any{"deleted": true})
}

// List trả mẫu còn sống cho menu "/" và màn cài đặt, theo phím tắt: pageID khác nil → mẫu chung và mẫu của Page đó
// (người gọi phải đọc được Page, không thì ErrNotFound); nil → mẫu chung và mẫu của mọi Page u đọc được. q khớp tiền
// tố phím tắt hay tiêu đề (không phân biệt hoa thường; rỗng = mọi mẫu). Tối đa 200 mẫu.
func List(ctx context.Context, tx pgx.Tx, u identity.AuthUser, q string, pageID *uuid.UUID) ([]Template, error) {
	params := store.ListReplyTemplatesParams{Prefix: strings.TrimSpace(q), Lim: listLimit, Pages: []uuid.UUID{}}
	if pageID != nil {
		if err := access.Require(u, permRead, *pageID); err != nil {
			return nil, err //nolint:wrapcheck // sentinel của access
		}
		params.Pages = []uuid.UUID{*pageID}
	} else {
		all, ids := access.VisiblePages(u)
		params.AllPages = all
		params.Pages = append(params.Pages, ids...)
	}
	rows, err := store.New(tx).ListReplyTemplates(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("liệt kê mẫu trả lời: %w", err)
	}
	out := make([]Template, 0, len(rows))
	for _, r := range rows {
		t, err := fromRow(store.GetReplyTemplateRow(r))
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// checkManage: mẫu chung (pageID nil) cần conversation.settings phạm vi all; mẫu của Page cần quyền đó trên Page và Page
// phải có (không có → ErrInvalid).
func checkManage(ctx context.Context, tx pgx.Tx, u identity.AuthUser, pageID *uuid.UUID) error {
	if pageID == nil {
		if all, _ := identity.ScopesFor(u, permSettings); !all {
			return ErrForbidden
		}
		return nil
	}
	if !identity.Can(u, permSettings, access.PageScope(*pageID)) {
		return ErrForbidden
	}
	if _, err := channel.Account(ctx, tx, *pageID); err != nil {
		if errors.Is(err, channel.ErrAccountNotFound) {
			return ErrInvalid
		}
		return fmt.Errorf("đọc Page của mẫu: %w", err)
	}
	return nil
}

// prepare kiểm in (validate) và trả cột quick_replies đã mã hóa.
func prepare(in Input) ([]byte, error) {
	if err := validate(in); err != nil {
		return nil, err
	}
	qr, err := json.Marshal(quickRepliesJSON(in.QuickReplies))
	if err != nil {
		return nil, fmt.Errorf("mã hóa nút của mẫu: %w", err)
	}
	return qr, nil
}

// validate kiểm dữ liệu mẫu đã chuẩn hóa (phím tắt chữ thường, tiêu đề đã bỏ khoảng trắng) và biến của thân, nút.
func validate(in Input) error {
	if !shortcutPattern.MatchString(in.Shortcut) || in.Title == "" || utf8.RuneCountInString(in.Title) > maxTitleRunes ||
		strings.TrimSpace(in.Body) == "" || utf8.RuneCountInString(in.Body) > maxBodyRunes ||
		len(in.QuickReplies) > maxQuickReplies {
		return ErrInvalid
	}
	if _, err := Variables(in.Body); err != nil {
		return err
	}
	for _, qr := range in.QuickReplies {
		if qr.Title == "" || utf8.RuneCountInString(qr.Title) > maxQRTitleRunes || qr.Payload == "" ||
			utf8.RuneCountInString(qr.Payload) > maxQRPayload {
			return ErrInvalid
		}
		if _, err := Variables(qr.Title); err != nil {
			return err
		}
	}
	return nil
}

func apply(cur Template, p Patch) Template {
	next := cur
	if p.SetPage {
		next.PageID = p.PageID
	}
	if p.Shortcut != nil {
		next.Shortcut = strings.ToLower(strings.TrimSpace(*p.Shortcut))
	}
	if p.Title != nil {
		next.Title = strings.TrimSpace(*p.Title)
	}
	if p.Body != nil {
		next.Body = *p.Body
	}
	if p.SetAttachment {
		next.AttachmentFileID = p.AttachmentFileID
	}
	if p.QuickReplies != nil {
		next.QuickReplies = *p.QuickReplies
	}
	return next
}

// UsableOnPage báo mẫu id còn sống và dùng được trên Page pageID (mẫu chung, hay mẫu riêng của đúng Page đó); đọc không
// khóa (ADR-46 nhóm 6: bên gọi có thể đã khóa page_settings, đứng sau reply_templates). Dùng cho mẫu xin địa chỉ của cài
// đặt Page (Task 160).
func UsableOnPage(ctx context.Context, tx pgx.Tx, id, pageID uuid.UUID) (bool, error) {
	row, err := store.New(tx).GetReplyTemplate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("đọc mẫu trả lời: %w", err)
	}
	return !row.ChannelAccountID.Valid || uuid.UUID(row.ChannelAccountID.Bytes) == pageID, nil
}

func lockTemplate(ctx context.Context, q *store.Queries, id uuid.UUID) (Template, error) {
	row, err := q.LockReplyTemplate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Template{}, ErrNotFound
	}
	if err != nil {
		return Template{}, fmt.Errorf("khóa mẫu trả lời: %w", err)
	}
	return fromRow(store.GetReplyTemplateRow(row))
}

// writeErr đổi lỗi ghi: trùng phím tắt → ErrShortcutTaken; khóa ngoại (tệp, Page không có) → ErrInvalid.
func writeErr(err error, what string) error {
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pe.Code {
		case pgUniqueViolation:
			return ErrShortcutTaken
		case pgForeignKeyViolation:
			return ErrInvalid
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

// audit ghi một dòng audit: chỉ phím tắt, Page, tiêu đề và tên các trường thân/nút/tệp (audit ghi tên trường đổi).
func (w Writer) audit(ctx context.Context, tx pgx.Tx, u identity.AuthUser, action string, id uuid.UUID,
	before, after map[string]any,
) error {
	e := identity.AuditEntry{TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), EntityType: EntityTemplate}
	e.Action, e.EntityID, e.RequestID = action, id.String(), w.RequestID
	e.Before, e.After = before, after
	if err := identity.RecordAudit(ctx, tx, e); err != nil {
		return fmt.Errorf("ghi audit %s: %w", action, err)
	}
	return nil
}

// auditView là dạng của mẫu trong audit: thân, nút, tệp chỉ ghi dấu vân tay để changed_fields nêu đúng tên trường mà
// không chép nội dung.
func auditView(t Template) map[string]any {
	page := ""
	if t.PageID != nil {
		page = t.PageID.String()
	}
	file := ""
	if t.AttachmentFileID != nil {
		file = t.AttachmentFileID.String()
	}
	qr, _ := json.Marshal(quickRepliesJSON(t.QuickReplies)) //nolint:errchkjson // chuỗi, không lỗi
	return map[string]any{
		"shortcut": t.Shortcut, "title": t.Title, "page_id": page, "attachment_file_id": file,
		"body": fingerprint(t.Body), "quick_replies": fingerprint(string(qr)),
	}
}

// quickReply là dạng lưu của một nút trong cột quick_replies.
type quickReply struct {
	Title   string `json:"title"`
	Payload string `json:"payload"`
}

func quickRepliesJSON(in []channel.QuickReply) []quickReply {
	out := make([]quickReply, 0, len(in))
	for _, q := range in {
		out = append(out, quickReply{Title: q.Title, Payload: q.Payload})
	}
	return out
}

func fromRow(r store.GetReplyTemplateRow) (Template, error) {
	var qrs []quickReply
	if err := json.Unmarshal(r.QuickReplies, &qrs); err != nil {
		return Template{}, fmt.Errorf("đọc nút của mẫu: %w", err)
	}
	t := Template{
		ID: r.ID, Shortcut: r.Shortcut, Title: r.Title, Body: r.Body, QuickReplies: make([]channel.QuickReply, 0, len(qrs)),
		Version: r.Version, CreatedAt: r.CreatedAt,
	}
	if r.ChannelAccountID.Valid {
		t.PageID = new(uuid.UUID(r.ChannelAccountID.Bytes))
	}
	if r.AttachmentFileID.Valid {
		t.AttachmentFileID = new(uuid.UUID(r.AttachmentFileID.Bytes))
	}
	for _, q := range qrs {
		t.QuickReplies = append(t.QuickReplies, channel.QuickReply{Title: q.Title, Payload: q.Payload})
	}
	return t, nil
}

// fingerprint là 16 ký tự hex đầu của SHA-256 của s: đổi khi nội dung đổi, không lộ nội dung.
func fingerprint(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}
