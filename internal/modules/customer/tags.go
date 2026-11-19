package customer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
)

// Nguồn của một liên kết nhãn (cột customer_tag_links.source).
const (
	// TagSourceStaff: nhân viên gắn tay (gỡ tay được).
	TagSourceStaff = "staff"
	// TagSourceRule: quy tắc nhãn gắn (Task M2-25); chỉ quy tắc gỡ.
	TagSourceRule = "rule"
)

const (
	aggregateTag     = "customer_tag"
	maxTagNameRunes  = 50
	tagNameUniqueKey = "customer_tags_name_key"
)

// Tag là một nhãn khách của shop. Color là khóa màu của console (một trong TagColors).
type Tag struct {
	ID      uuid.UUID
	Name    string
	Color   string
	Version int64
}

// TagLink là một nhãn đang gắn trên nhóm khách: Source là TagSourceStaff nếu có ít nhất một dòng gắn tay trong nhóm,
// không thì TagSourceRule.
type TagLink struct {
	Tag    Tag
	Source string
}

// TagColors trả 10 khóa màu nhãn, đúng CHECK customer_tags.color (migration 0201; console ánh xạ khóa → màu thật).
// Hợp đồng API nhận chuỗi rộng hơn, nên đường ghi kiểm theo danh sách này trước khi ghi.
func TagColors() []string {
	return []string{"gray", "red", "orange", "yellow", "green", "teal", "blue", "purple", "pink", "brown"}
}

// ListTags trả mọi nhãn khách của tenant của tx, theo tên.
func ListTags(ctx context.Context, tx pgx.Tx) ([]Tag, error) {
	rows, err := store.New(tx).ListTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc nhãn khách: %w", err)
	}
	out := make([]Tag, len(rows))
	for i, r := range rows {
		out[i] = tagOf(r.ID, r.Name, r.Color, r.Version)
	}
	return out, nil
}

func tagOf(id uuid.UUID, name, color string, version int64) Tag {
	return Tag{ID: id, Name: name, Color: color, Version: version}
}

// CreateTag tạo nhãn tên name (bỏ khoảng trắng hai đầu, 1–50 ký tự) màu color (một trong TagColors; khác →
// ErrInvalidInput) trong tx của bên gọi; trùng tên một nhãn có sẵn (phân biệt hoa thường như unique của bảng) →
// ErrTagNameTaken. Ghi audit customer_tag.create (tên trường).
func (w Writer) CreateTag(ctx context.Context, tx pgx.Tx, name, color string, a Actor, now time.Time) (Tag, error) {
	name, err := validTag(name, color)
	if err != nil {
		return Tag{}, err
	}
	r, err := store.New(tx).InsertTag(ctx, store.InsertTagParams{Name: name, Color: color, Now: now})
	if tagNameTaken(err) {
		return Tag{}, ErrTagNameTaken
	}
	if err != nil {
		return Tag{}, fmt.Errorf("tạo nhãn khách: %w", err)
	}
	tag := tagOf(r.ID, r.Name, r.Color, r.Version)
	after := fieldsAudit([]string{"name", "color"})
	after["version"] = tag.Version
	if err := w.auditEntity(ctx, tx, r.TenantID, a, "customer_tag.create", aggregateTag, tag.ID, nil, after); err != nil {
		return Tag{}, err
	}
	return tag, nil
}

// UpdateTag đổi tên, màu (nil giữ nguyên) của nhãn id khi version còn khớp, trong tx riêng của bên gọi (ADR-46 mục e:
// không chung transaction với bảng khác). Không có (hay của tenant khác) → ErrNotFound; version lệch →
// ErrVersionConflict; tên, màu sai → ErrInvalidInput; tên trùng → ErrTagNameTaken. Ghi audit customer_tag.update.
func (w Writer) UpdateTag(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, name, color *string, a Actor,
	now time.Time,
) (Tag, error) {
	q := store.New(tx)
	cur, err := q.GetTag(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tag{}, ErrNotFound
	}
	if err != nil {
		return Tag{}, fmt.Errorf("đọc nhãn khách: %w", err)
	}
	if cur.Version != version {
		return Tag{}, ErrVersionConflict
	}
	next := Tag(cur)
	var fields []string
	if name != nil && *name != next.Name {
		next.Name = *name
		fields = append(fields, "name")
	}
	if color != nil && *color != next.Color {
		next.Color = *color
		fields = append(fields, "color")
	}
	if next.Name, err = validTag(next.Name, next.Color); err != nil {
		return Tag{}, err
	}
	r, err := q.UpdateTag(ctx, store.UpdateTagParams{Name: next.Name, Color: next.Color, Now: now, ID: id, Version: version})
	switch {
	case errors.Is(err, pgx.ErrNoRows): // sửa song song vừa commit giữa lúc đọc và lúc sửa
		return Tag{}, ErrVersionConflict
	case tagNameTaken(err):
		return Tag{}, ErrTagNameTaken
	case err != nil:
		return Tag{}, fmt.Errorf("sửa nhãn khách: %w", err)
	}
	tag := tagOf(r.ID, r.Name, r.Color, r.Version)
	after := fieldsAudit(fields)
	after["version"] = tag.Version
	if err := w.auditEntity(ctx, tx, r.TenantID, a, "customer_tag.update", aggregateTag, id,
		map[string]any{"version": version}, after); err != nil {
		return Tag{}, err
	}
	return tag, nil
}

// AddTag gắn nhãn tagID vào **gốc** của khách customerID (id là gốc hay hồ sơ con) với nguồn source (TagSourceStaff
// hay TagSourceRule; khác → ErrInvalidInput), trong tx của bên gọi. Gốc đã có nhãn (nguồn nào cũng vậy) thì không làm
// gì. Khách hay nhãn không có (hay của tenant khác) → ErrNotFound. Có gắn thì ghi audit customer.tag_add (mã nhãn,
// nguồn) và báo hiệu customer.changed. Không khóa customer_tags (ADR-46 mục e).
func (w Writer) AddTag(ctx context.Context, tx pgx.Tx, customerID, tagID uuid.UUID, source string, a Actor,
	now time.Time,
) error {
	if err := w.check(); err != nil {
		return err
	}
	if source != TagSourceStaff && source != TagSourceRule {
		return ErrInvalidInput
	}
	root, err := RootOf(ctx, tx, customerID)
	if err != nil {
		return err
	}
	if err := tagExists(ctx, tx, tagID); err != nil {
		return err
	}
	tenantID, err := store.New(tx).InsertTagLink(ctx, store.InsertTagLinkParams{
		CustomerID: root, TagID: tagID, Source: source, CreatedBy: a.createdBy(), Now: now,
	})
	if errors.Is(err, pgx.ErrNoRows) { // gốc đã có nhãn
		return nil
	}
	if err != nil {
		return fmt.Errorf("gắn nhãn khách: %w", err)
	}
	return w.tagChanged(ctx, tx, tenantID, a, "customer.tag_add", root,
		map[string]any{"tag_id": tagID.String(), "source": source})
}

// RemoveTag gỡ nhãn tagID gắn tay (TagSourceStaff) khỏi mọi hồ sơ trong nhóm của khách customerID, trong tx của bên
// gọi. Dòng TagSourceRule không bao giờ bị gỡ: nhóm chỉ có nhãn đó do quy tắc → ErrTagRuleManaged. Nhóm chưa có nhãn
// → không làm gì. Khách hay nhãn không có → ErrNotFound. Có gỡ thì ghi audit customer.tag_remove và báo hiệu.
func (w Writer) RemoveTag(ctx context.Context, tx pgx.Tx, customerID, tagID uuid.UUID, a Actor) error {
	if err := w.check(); err != nil {
		return err
	}
	root, err := RootOf(ctx, tx, customerID)
	if err != nil {
		return err
	}
	if err := tagExists(ctx, tx, tagID); err != nil {
		return err
	}
	members, err := GroupMembers(ctx, tx, root)
	if err != nil {
		return err
	}
	q := store.New(tx)
	removed, err := q.DeleteStaffTagLinks(ctx, store.DeleteStaffTagLinksParams{Ids: members, TagID: tagID})
	if err != nil {
		return fmt.Errorf("gỡ nhãn khách: %w", err)
	}
	if len(removed) > 0 {
		return w.tagChanged(ctx, tx, removed[0], a, "customer.tag_remove", root, map[string]any{"tag_id": tagID.String()})
	}
	rules, err := q.CountRuleTagLinks(ctx, store.CountRuleTagLinksParams{Ids: members, TagID: tagID})
	if err != nil {
		return fmt.Errorf("đọc nhãn theo quy tắc: %w", err)
	}
	if rules > 0 {
		return ErrTagRuleManaged
	}
	return nil
}

// tagChanged ghi audit (mã nhãn, không giá trị cá nhân) và báo hiệu cho gốc root sau khi gắn hay gỡ nhãn.
func (w Writer) tagChanged(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, a Actor, action string, root uuid.UUID,
	after map[string]any,
) error {
	if err := w.audit(ctx, tx, tenantID, a, action, root, nil, after); err != nil {
		return err
	}
	return w.signal(ctx, tx, tenantID, root)
}

// groupTags đọc nhãn của nhóm hồ sơ members (mỗi nhãn một dòng, theo tên).
func groupTags(ctx context.Context, tx pgx.Tx, members []uuid.UUID) ([]TagLink, error) {
	rows, err := store.New(tx).ListGroupTags(ctx, members)
	if err != nil {
		return nil, fmt.Errorf("đọc nhãn của nhóm: %w", err)
	}
	out := make([]TagLink, 0, len(rows))
	for _, r := range rows {
		out = append(out, TagLink{
			Tag: tagOf(r.ID, r.Name, r.Color, r.Version), Source: r.Source,
		})
	}
	return out, nil
}

func tagExists(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := store.New(tx).GetTag(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("đọc nhãn khách: %w", err)
	}
	return nil
}

// validTag trả tên đã bỏ khoảng trắng hai đầu; tên rỗng, quá 50 ký tự, có ký tự điều khiển hay màu ngoài TagColors →
// ErrInvalidInput.
func validTag(name, color string) (string, error) {
	if !slices.Contains(TagColors(), color) {
		return "", ErrInvalidInput
	}
	return validText(name, maxTagNameRunes)
}

// tagNameTaken: err là vi phạm unique tên nhãn (customer_tags_name_key).
func tagNameTaken(err error) bool {
	pg, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pg.Code == "23505" && pg.ConstraintName == tagNameUniqueKey
}
