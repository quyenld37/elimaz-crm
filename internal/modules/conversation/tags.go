package conversation

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/calls"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Nhãn hội thoại dùng chung cả shop (Task 160, Q7): tên không trùng (không phân biệt hoa thường) giữa các nhãn còn
// sống, xóa mềm. Ai được tạo, sửa, xóa nhãn (conversation.settings phạm vi all, chủ shop 26/11) do lớp HTTP kiểm.

var (
	// ErrTagNotFound là lỗi khi nhãn không có hoặc đã xóa.
	ErrTagNotFound = errors.New("conversation: không có nhãn")
	// ErrTagNameTaken là lỗi khi đã có nhãn còn sống cùng tên (không phân biệt hoa thường).
	ErrTagNameTaken = errors.New("conversation: tên nhãn đã có")
	// ErrInvalidInput là lỗi khi tên, màu, thứ tự của nhãn hay cài đặt Page không hợp lệ.
	ErrInvalidInput = errors.New("conversation: dữ liệu không hợp lệ")
)

// EntityTag là loại thực thể của báo hiệu, audit của nhãn.
const EntityTag = "conversation_tag"

const (
	tagNameLiveIndex = "tags_name_live"
	maxTagName       = 40
	maxTagSort       = 1000
)

var tagColorRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// Tag là một nhãn còn sống (cùng kiểu với nhãn trong danh sách hội thoại của khách, Task 169).
type Tag = calls.Tag

// TagInput là nội dung một nhãn (tên được cắt khoảng trắng hai đầu).
type TagInput struct {
	Name, Color string
	Sort        int32
}

// TagPatch là phần sửa của một nhãn; trường nil giữ nguyên.
type TagPatch struct {
	Name, Color *string
	Sort        *int32
}

func (in TagInput) normalized() (TagInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > maxTagName || !tagColorRe.MatchString(in.Color) ||
		in.Sort < 0 || in.Sort > maxTagSort {
		return TagInput{}, ErrInvalidInput
	}
	return in, nil
}

// ListTags trả nhãn còn sống theo thứ tự hiển thị (sort, tên).
func ListTags(ctx context.Context, tx pgx.Tx) ([]Tag, error) {
	rows, err := store.New(tx).ListTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc nhãn hội thoại: %w", err)
	}
	out := make([]Tag, len(rows))
	for i := range rows {
		out[i] = Tag(rows[i])
	}
	return out, nil
}

// TaggedConversationIDs trả mã hội thoại mang nhãn còn sống tagID (nhãn đã xóa → rỗng); bên gọi tự lọc theo Page được
// xem (lọc theo nhãn của danh sách hội thoại, CH07).
func TaggedConversationIDs(ctx context.Context, tx pgx.Tx, tagID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := store.New(tx).ListTaggedConversationIDs(ctx, tagID)
	if err != nil {
		return nil, fmt.Errorf("đọc hội thoại theo nhãn: %w", err)
	}
	return ids, nil
}

func nameTaken(err error) bool {
	pe, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pe.Code == "23505" && pe.ConstraintName == tagNameLiveIndex
}

// CreateTag tạo nhãn; tên trùng → ErrTagNameTaken; dữ liệu sai → ErrInvalidInput. Audit conversation.tag_created.
func (w Writer) CreateTag(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, in TagInput) (Tag, error) {
	in, err := in.normalized()
	if err != nil {
		return Tag{}, err
	}
	r, err := store.New(tx).InsertTag(ctx, store.InsertTagParams{Name: in.Name, Color: in.Color, Sort: in.Sort})
	if nameTaken(err) {
		return Tag{}, ErrTagNameTaken
	}
	if err != nil {
		return Tag{}, fmt.Errorf("tạo nhãn hội thoại: %w", err)
	}
	t := Tag(r)
	return t, w.tagChanged(ctx, tx, actor, "conversation.tag_created", t.ID, nil, tagAudit(t))
}

// UpdateTag sửa nhãn id khi expectedVersion khớp (ErrVersionConflict); không có, đã xóa → ErrTagNotFound.
func (w Writer) UpdateTag(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID, p TagPatch,
	expectedVersion int32,
) (Tag, error) {
	q := store.New(tx)
	cur, err := lockTag(ctx, q, id)
	if err != nil {
		return Tag{}, err
	}
	if cur.Version != expectedVersion {
		return Tag{}, ErrVersionConflict
	}
	in := TagInput{Name: cur.Name, Color: cur.Color, Sort: cur.Sort}
	if p.Name != nil {
		in.Name = *p.Name
	}
	if p.Color != nil {
		in.Color = *p.Color
	}
	if p.Sort != nil {
		in.Sort = *p.Sort
	}
	if in, err = in.normalized(); err != nil {
		return Tag{}, err
	}
	if in.Name == cur.Name && in.Color == cur.Color && in.Sort == cur.Sort {
		return cur, nil
	}
	r, err := q.UpdateTag(ctx, store.UpdateTagParams{Name: in.Name, Color: in.Color, Sort: in.Sort, ID: id})
	if nameTaken(err) {
		return Tag{}, ErrTagNameTaken
	}
	if err != nil {
		return Tag{}, fmt.Errorf("sửa nhãn hội thoại: %w", err)
	}
	t := Tag(r)
	return t, w.tagChanged(ctx, tx, actor, "conversation.tag_updated", id, tagAudit(cur), tagAudit(t))
}

// DeleteTag xóa mềm nhãn id (gỡ khỏi danh sách và khỏi bộ lọc; dòng gắn giữ lại làm lịch sử). Không có, đã xóa →
// ErrTagNotFound.
func (w Writer) DeleteTag(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID) error {
	q := store.New(tx)
	cur, err := lockTag(ctx, q, id)
	if err != nil {
		return err
	}
	if err := q.SoftDeleteTag(ctx, id); err != nil {
		return fmt.Errorf("xóa nhãn hội thoại: %w", err)
	}
	return w.tagChanged(ctx, tx, actor, "conversation.tag_deleted", id, tagAudit(cur), nil)
}

func lockTag(ctx context.Context, q *store.Queries, id uuid.UUID) (Tag, error) {
	r, err := q.LockTag(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tag{}, ErrTagNotFound
	}
	if err != nil {
		return Tag{}, fmt.Errorf("khóa nhãn hội thoại: %w", err)
	}
	return Tag(r), nil
}

func tagAudit(t Tag) map[string]any {
	return map[string]any{"name": t.Name, "color": t.Color, "sort": t.Sort}
}

// tagChanged ghi audit và báo hiệu conversation.changed thực thể conversation_tag (mọi người có conversation.read).
func (w Writer) tagChanged(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, action string, id uuid.UUID,
	before, after map[string]any,
) error {
	if err := w.staffAudit(ctx, tx, actor, identity.AuditEntry{
		Action: action, EntityType: EntityTag, EntityID: id.String(), Before: before, After: after,
	}); err != nil {
		return err
	}
	if w.Realtime == nil {
		return nil
	}
	if err := w.Realtime.Publish(ctx, tx, realtime.Signal{
		TenantID: actor.TenantID, Topic: TopicConversationChanged, Requires: assign.PermRead,
		Entity: realtime.Entity{Type: EntityTag, ID: id.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu nhãn hội thoại: %w", err)
	}
	return nil
}

// AddTag gắn nhãn còn sống tagID cho hội thoại convID (cần conversation.reply trên Page); gắn lại không lỗi, không
// báo hiệu lại. Nhãn không có, đã xóa → ErrTagNotFound; hội thoại không có, không quyền → ErrNotFound.
func (w Writer) AddTag(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, convID, tagID uuid.UUID) error {
	return w.tagLink(ctx, tx, actor, convID, tagID, func(q *store.Queries) (int64, error) {
		return q.AddTagLink(ctx, store.AddTagLinkParams{
			ConversationID: convID, TagID: tagID, CreatedBy: pgtype.UUID{Bytes: actor.UserID, Valid: true},
		})
	})
}

// RemoveTag gỡ nhãn tagID khỏi hội thoại convID (cần conversation.reply trên Page); gỡ nhãn không gắn không lỗi.
func (w Writer) RemoveTag(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, convID, tagID uuid.UUID) error {
	return w.tagLink(ctx, tx, actor, convID, tagID, func(q *store.Queries) (int64, error) {
		return q.RemoveTagLink(ctx, store.RemoveTagLinkParams{ConversationID: convID, TagID: tagID})
	})
}

// tagLink: nhóm 6 ADR-46 — giữ nhãn (FOR SHARE) trước, rồi hội thoại, rồi dòng gắn.
func (w Writer) tagLink(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, convID, tagID uuid.UUID,
	apply func(*store.Queries) (int64, error),
) error {
	q := store.New(tx)
	if _, err := q.ShareLockTag(ctx, tagID); errors.Is(err, pgx.ErrNoRows) {
		return ErrTagNotFound
	} else if err != nil {
		return fmt.Errorf("giữ nhãn hội thoại: %w", err)
	}
	page, err := q.GetConversationPage(ctx, convID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("đọc hội thoại: %w", err)
	}
	if err := access.Require(actor, assign.PermReply, page); err != nil {
		return err //nolint:wrapcheck // sentinel của module
	}
	n, err := apply(q)
	if err != nil {
		return fmt.Errorf("gắn/gỡ nhãn hội thoại: %w", err)
	}
	if n == 0 {
		return nil
	}
	return assign.Signal(ctx, tx, w.Realtime, actor.TenantID, page, assign.EntityConversation, convID) //nolint:wrapcheck // assign đã bọc
}

// staffAudit ghi e với người làm là nhân viên actor (tenant, mã người, RequestID của yêu cầu).
func (w Writer) staffAudit(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, e identity.AuditEntry) error {
	e.TenantID, e.ActorType, e.ActorID, e.RequestID = actor.TenantID, "staff", actor.UserID.String(), w.RequestID
	if err := identity.RecordAudit(ctx, tx, e); err != nil {
		return fmt.Errorf("ghi audit %s: %w", e.Action, err)
	}
	return nil
}
