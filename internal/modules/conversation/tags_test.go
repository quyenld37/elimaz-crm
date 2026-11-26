package conversation_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/conversation/convtest"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// tagEnv: tenant A có Page 1, Page 2; quản lý (đọc, trả lời, cài đặt toàn shop), sale (đọc, trả lời Page 1).
type tagEnv struct {
	t            *testing.T
	s            *testkit.Staff
	tenant       uuid.UUID
	page1, page2 uuid.UUID
	bus          *realtimetest.RecordingBus
}

const (
	chatManager = "quanly@elimaz.test"
	chatSale    = "sale@elimaz.test"
)

func newTagEnv(t *testing.T) *tagEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &tagEnv{t: t, s: s, tenant: s.Tenant(t, "A"), bus: &realtimetest.RecordingBus{}}
	e.page1 = s.ChannelAccount(t, e.tenant, "fakechat", "page-1", "Page 1")
	e.page2 = s.ChannelAccount(t, e.tenant, "fakechat", "page-2", "Page 2")
	convtest.Staff(t, s, e.tenant, uuid.Nil, chatManager, "conversation.read", "conversation.reply", "conversation.settings")
	convtest.Staff(t, s, e.tenant, e.page1, chatSale, "conversation.read", "conversation.reply")
	return e
}

func (e *tagEnv) w() conversation.Writer { return conversation.Writer{Realtime: e.bus} }

// tx chạy fn bằng crm_app trong tenant A với Writer có bus ghi lại báo hiệu.
func (e *tagEnv) write(fn func(tx pgx.Tx, w conversation.Writer) error) error {
	return tenancy.WithTenant(e.t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) error { return fn(tx, e.w()) }) //nolint:wrapcheck // test
}

func (e *tagEnv) createTag(name string) (conversation.Tag, error) {
	var tag conversation.Tag
	u := convtest.Load(e.t, e.s, chatManager)
	err := tenancy.WithTenant(e.t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) (err error) {
		tag, err = e.w().CreateTag(e.t.Context(), tx, u, conversation.TagInput{Name: name, Color: "green"})
		return err //nolint:wrapcheck // test
	})
	return tag, err //nolint:wrapcheck // test
}

func (e *tagEnv) tagged(tag uuid.UUID) []uuid.UUID {
	var ids []uuid.UUID
	if err := tenancy.WithTenant(e.t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) (err error) {
		ids, err = conversation.TaggedConversationIDs(e.t.Context(), tx, tag)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return ids
}

func (e *tagEnv) link(email string, add bool, conv, tag uuid.UUID) error {
	u := convtest.Load(e.t, e.s, email)
	return tenancy.WithTenant(e.t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) error { //nolint:wrapcheck // test
		if add {
			return e.w().AddTag(e.t.Context(), tx, u, conv, tag)
		}
		return e.w().RemoveTag(e.t.Context(), tx, u, conv, tag)
	})
}

// CH07 nhãn: tạo "Đã lên đơn", gắn cho 3 hội thoại → lọc ra 3; gắn lại không lỗi; tên trùng khác hoa thường → 409;
// gỡ cái không gắn không lỗi; sale không có quyền trên Page 2 → ErrNotFound; xóa mềm → lọc ra 0, không còn trong danh
// sách; nhãn đã xóa không gắn được.
func TestTags_CH07(t *testing.T) {
	t.Parallel()
	e := newTagEnv(t)
	tag, err := e.createTag("  Đã lên đơn ")
	if err != nil || tag.Name != "Đã lên đơn" || tag.Version != 1 {
		t.Fatalf("tạo nhãn: %+v %v", tag, err)
	}
	convs := []uuid.UUID{
		convtest.Conversation(t, e.s, e.tenant, e.page1, "t-1"), convtest.Conversation(t, e.s, e.tenant, e.page1, "t-2"),
		convtest.Conversation(t, e.s, e.tenant, e.page2, "t-3"),
	}
	for _, c := range convs {
		if err := e.link(chatManager, true, c, tag.ID); err != nil {
			t.Fatal(err)
		}
	}
	signals := len(e.bus.Signals())
	if err := e.link(chatSale, true, convs[0], tag.ID); err != nil {
		t.Fatalf("gắn lại: %v", err)
	}
	if len(e.bus.Signals()) != signals {
		t.Fatal("gắn lại vẫn báo hiệu")
	}
	if got := e.tagged(tag.ID); len(got) != 3 {
		t.Fatalf("lọc theo nhãn = %d", len(got))
	}
	if err := e.link(chatSale, true, convs[2], tag.ID); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("sale gắn nhãn hội thoại Page 2: %v", err)
	}
	if err := e.link(chatSale, false, convtest.Conversation(t, e.s, e.tenant, e.page1, "t-4"), tag.ID); err != nil {
		t.Fatalf("gỡ nhãn không gắn: %v", err)
	}
	if _, err := e.createTag("đã LÊN đơn"); !errors.Is(err, conversation.ErrTagNameTaken) {
		t.Fatalf("tên trùng: %v", err)
	}
	if _, err := e.createTag(""); !errors.Is(err, conversation.ErrInvalidInput) {
		t.Fatalf("tên rỗng: %v", err)
	}
	u := convtest.Load(t, e.s, chatManager)
	if err := e.write(func(tx pgx.Tx, w conversation.Writer) error {
		return w.DeleteTag(t.Context(), tx, u, tag.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if got := e.tagged(tag.ID); len(got) != 0 {
		t.Fatalf("lọc theo nhãn đã xóa = %d", len(got))
	}
	var tags []conversation.Tag
	if err := e.write(func(tx pgx.Tx, _ conversation.Writer) (err error) {
		tags, err = conversation.ListTags(t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil || len(tags) != 0 {
		t.Fatalf("danh sách sau khi xóa: %v %v", tags, err)
	}
	if err := e.link(chatManager, true, convs[0], tag.ID); !errors.Is(err, conversation.ErrTagNotFound) {
		t.Fatalf("gắn nhãn đã xóa: %v", err)
	}
	if _, err := e.createTag("Đã lên đơn"); err != nil {
		t.Fatalf("tạo lại tên của nhãn đã xóa: %v", err)
	}
	if n := e.s.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action LIKE 'conversation.tag_%'`); n != 3 {
		t.Fatalf("audit nhãn = %d", n)
	}
}

// Sửa nhãn: version cũ → ErrVersionConflict; đổi màu → version + 1; nhãn không có → ErrTagNotFound.
func TestTags_Update(t *testing.T) {
	t.Parallel()
	e := newTagEnv(t)
	tag, err := e.createTag("Khách quen")
	if err != nil {
		t.Fatal(err)
	}
	u := convtest.Load(t, e.s, chatManager)
	update := func(id uuid.UUID, v int32) (conversation.Tag, error) {
		var out conversation.Tag
		color := "red"
		err := e.write(func(tx pgx.Tx, w conversation.Writer) (err error) {
			out, err = w.UpdateTag(t.Context(), tx, u, id, conversation.TagPatch{Color: &color}, v)
			return err //nolint:wrapcheck // test
		})
		return out, err
	}
	if _, err := update(tag.ID, 5); !errors.Is(err, conversation.ErrVersionConflict) {
		t.Fatalf("version cũ: %v", err)
	}
	if got, err := update(tag.ID, 1); err != nil || got.Color != "red" || got.Version != 2 || got.Name != "Khách quen" {
		t.Fatalf("sửa: %+v %v", got, err)
	}
	if _, err := update(uuid.New(), 1); !errors.Is(err, conversation.ErrTagNotFound) {
		t.Fatalf("nhãn không có: %v", err)
	}
}
