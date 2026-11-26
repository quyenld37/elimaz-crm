package templates_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/templates"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

func (e *tplEnv) create(u identity.AuthUser, in templates.Input) (templates.Template, error) {
	e.t.Helper()
	var out templates.Template
	err := e.tx(func(tx pgx.Tx) error {
		var err error
		out, err = templates.Writer{RequestID: "req-1"}.Create(e.t.Context(), tx, u, in)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func (e *tplEnv) mustCreate(u identity.AuthUser, in templates.Input) templates.Template {
	e.t.Helper()
	tp, err := e.create(u, in)
	if err != nil {
		e.t.Fatalf("tạo mẫu %s: %v", in.Shortcut, err)
	}
	return tp
}

func (e *tplEnv) update(u identity.AuthUser, id uuid.UUID, p templates.Patch) (templates.Template, error) {
	e.t.Helper()
	var out templates.Template
	err := e.tx(func(tx pgx.Tx) error {
		var err error
		out, err = templates.Writer{}.Update(e.t.Context(), tx, u, id, p)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func (e *tplEnv) remove(u identity.AuthUser, id uuid.UUID) error {
	e.t.Helper()
	return e.tx(func(tx pgx.Tx) error {
		return templates.Writer{}.Delete(e.t.Context(), tx, u, id)
	})
}

func (e *tplEnv) list(u identity.AuthUser, q string, page *uuid.UUID) ([]string, error) {
	e.t.Helper()
	var out []string
	err := e.tx(func(tx pgx.Tx) error {
		items, err := templates.List(e.t.Context(), tx, u, q, page)
		for _, it := range items {
			out = append(out, it.Shortcut)
		}
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func xin() templates.Input {
	return templates.Input{Shortcut: "xindiachi", Title: "Xin địa chỉ", Body: "Chào {ten_khach}, cho shop xin địa chỉ nhận hàng ạ"}
}

// Tạo: lưu đủ trường, version 1, audit reply_template.create có phím tắt, không có nội dung thân; phím tắt trùng (khác hoa thường,
// kể cả mẫu của Page) → ErrShortcutTaken; biến lạ {abc} → ErrUnknownVariable; dữ liệu sai → ErrInvalid; mẫu đã xóa
// thì dùng lại phím tắt được.
func TestTemplateCreate_ValidationAndUnique(t *testing.T) {
	t.Parallel()
	e := newTplEnv(t)
	in := xin()
	in.QuickReplies = []channel.QuickReply{{Title: "Có", Payload: "YES"}}
	tp := e.mustCreate(e.owner, in)
	if tp.Version != 1 || tp.PageID != nil || tp.Body != in.Body || len(tp.QuickReplies) != 1 || tp.QuickReplies[0].Payload != "YES" {
		t.Fatalf("mẫu = %+v", tp)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'reply_template.create' AND entity_id = $1
		AND after->>'shortcut' = 'xindiachi' AND position('nhận hàng' IN after::text) = 0`, tp.ID.String()); n != 1 {
		t.Fatalf("audit tạo = %d", n)
	}
	dup := xin()
	dup.Shortcut = "XinDiaChi"
	dup.PageID = &e.pageA
	if _, err := e.create(e.owner, dup); !errors.Is(err, templates.ErrShortcutTaken) {
		t.Fatalf("phím tắt trùng hoa thường: %v", err)
	}
	for _, c := range []struct {
		name string
		mod  func(*templates.Input)
		want error
	}{
		{"biến lạ", func(i *templates.Input) { i.Body = "Chào {abc}" }, templates.ErrUnknownVariable},
		{"biến lạ trong nút", func(i *templates.Input) { i.QuickReplies = []channel.QuickReply{{Title: "{abc}", Payload: "P"}} }, templates.ErrUnknownVariable},
		{"phím tắt có dấu cách", func(i *templates.Input) { i.Shortcut = "xin dia chi" }, templates.ErrInvalid},
		{"phím tắt 33 ký tự", func(i *templates.Input) { i.Shortcut = "a123456789012345678901234567890bc" }, templates.ErrInvalid},
		{"thân rỗng", func(i *templates.Input) { i.Body = "   " }, templates.ErrInvalid},
		{"tiêu đề rỗng", func(i *templates.Input) { i.Title = "" }, templates.ErrInvalid},
		{"14 nút", func(i *templates.Input) { i.QuickReplies = make([]channel.QuickReply, 14) }, templates.ErrInvalid},
		{"nút tiêu đề 21 ký tự", func(i *templates.Input) {
			i.QuickReplies = []channel.QuickReply{{Title: "Đặt hàng ngay hôm nay!", Payload: "P"}}
		}, templates.ErrInvalid},
		{"tệp không có", func(i *templates.Input) { f := uuid.New(); i.AttachmentFileID = &f }, templates.ErrInvalid},
		{"Page không có", func(i *templates.Input) { p := uuid.New(); i.PageID = &p }, templates.ErrInvalid},
	} {
		in := xin()
		in.Shortcut = "khac"
		c.mod(&in)
		if _, err := e.create(e.owner, in); !errors.Is(err, c.want) {
			t.Errorf("%s: lỗi %v, muốn %v", c.name, err, c.want)
		}
	}
	ok := xin()
	ok.Shortcut = "ma_don-1"
	ok.Body = "Đơn {ma_don} của {ten_khach} ({ten_page}, {nhan_vien}, {ma_van_don}) {không phải biến} {}"
	e.mustCreate(e.owner, ok)
	if err := e.remove(e.owner, tp.ID); err != nil {
		t.Fatal(err)
	}
	e.mustCreate(e.owner, xin())
}

// Quyết định chủ shop 26/11: mẫu chung chỉ người có conversation.settings phạm vi all tạo/sửa/xóa; người có quyền chỉ
// ở Page A tạo/sửa/xóa được mẫu của Page A, không được mẫu chung, mẫu của Page B, không chuyển mẫu Page A thành chung.
func TestTemplatePermissions_CommonNeedsAllPages(t *testing.T) {
	t.Parallel()
	e := newTplEnv(t)
	if _, err := e.create(e.pageAdmin, xin()); !errors.Is(err, templates.ErrForbidden) {
		t.Fatalf("Page A tạo mẫu chung: %v", err)
	}
	inB := xin()
	inB.PageID = &e.pageB
	if _, err := e.create(e.pageAdmin, inB); !errors.Is(err, templates.ErrForbidden) {
		t.Fatalf("Page A tạo mẫu Page B: %v", err)
	}
	if _, err := e.create(e.reader, func() templates.Input { i := xin(); i.PageID = &e.pageA; return i }()); !errors.Is(err, templates.ErrForbidden) {
		t.Fatalf("chỉ đọc tạo mẫu: %v", err)
	}
	inA := xin()
	inA.PageID = &e.pageA
	mine := e.mustCreate(e.pageAdmin, inA)
	title := "Xin địa chỉ (Page A)"
	if got, err := e.update(e.pageAdmin, mine.ID, templates.Patch{Title: &title, ExpectedVersion: 1}); err != nil || got.Version != 2 || got.Title != title {
		t.Fatalf("Page A sửa mẫu Page A: %+v %v", got, err)
	}
	if _, err := e.update(e.pageAdmin, mine.ID, templates.Patch{SetPage: true, ExpectedVersion: 2}); !errors.Is(err, templates.ErrForbidden) {
		t.Fatalf("Page A chuyển mẫu thành chung: %v", err)
	}
	common := e.mustCreate(e.owner, func() templates.Input { i := xin(); i.Shortcut = "chao"; return i }())
	if _, err := e.update(e.pageAdmin, common.ID, templates.Patch{Title: &title, ExpectedVersion: 1}); !errors.Is(err, templates.ErrForbidden) {
		t.Fatalf("Page A sửa mẫu chung: %v", err)
	}
	if err := e.remove(e.pageAdmin, common.ID); !errors.Is(err, templates.ErrForbidden) {
		t.Fatalf("Page A xóa mẫu chung: %v", err)
	}
	if err := e.remove(e.pageAdmin, mine.ID); err != nil {
		t.Fatalf("Page A xóa mẫu Page A: %v", err)
	}
	if _, err := e.update(e.owner, common.ID, templates.Patch{SetPage: true, PageID: &e.pageB, ExpectedVersion: 1}); err != nil {
		t.Fatalf("owner chuyển mẫu chung sang Page B: %v", err)
	}
}

// Sửa: version cũ → ErrVersionConflict; chỉ đổi trường gửi lên; audit reply_template.update ghi tên trường đổi; xóa
// mềm: lần hai, sửa sau khi xóa → ErrNotFound; mẫu tenant khác không thấy.
func TestTemplateUpdateDelete_Version(t *testing.T) {
	t.Parallel()
	e := newTplEnv(t)
	tp := e.mustCreate(e.owner, xin())
	body := "Chào {ten_khach} ạ"
	got, err := e.update(e.owner, tp.ID, templates.Patch{Body: &body, ExpectedVersion: 1})
	if err != nil || got.Version != 2 || got.Body != body || got.Title != tp.Title || got.Shortcut != tp.Shortcut {
		t.Fatalf("sửa thân: %+v %v", got, err)
	}
	if _, err := e.update(e.owner, tp.ID, templates.Patch{Body: &body, ExpectedVersion: 1}); !errors.Is(err, templates.ErrVersionConflict) {
		t.Fatalf("version cũ: %v", err)
	}
	bad := "Chào {abc}"
	if _, err := e.update(e.owner, tp.ID, templates.Patch{Body: &bad, ExpectedVersion: 2}); !errors.Is(err, templates.ErrUnknownVariable) {
		t.Fatalf("sửa có biến lạ: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'reply_template.update' AND entity_id = $1
		AND changed_fields = '{body}'`, tp.ID.String()); n != 1 {
		t.Fatalf("audit sửa = %d", n)
	}
	if err := e.remove(e.owner, tp.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.remove(e.owner, tp.ID); !errors.Is(err, templates.ErrNotFound) {
		t.Fatalf("xóa lần hai: %v", err)
	}
	if _, err := e.update(e.owner, tp.ID, templates.Patch{Body: &body, ExpectedVersion: 3}); !errors.Is(err, templates.ErrNotFound) {
		t.Fatalf("sửa mẫu đã xóa: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'reply_template.delete' AND entity_id = $1`, tp.ID.String()); n != 1 {
		t.Fatalf("audit xóa = %d", n)
	}
	other := e.mustCreate(e.owner, xin())
	ownerB := e.owner
	ownerB.TenantID = e.other
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.other, func(tx pgx.Tx) error {
		return templates.Writer{}.Delete(t.Context(), tx, ownerB, other.ID)
	}); !errors.Is(err, templates.ErrNotFound) {
		t.Fatalf("tenant B xóa mẫu của A: %v", err)
	}
}

// Danh sách cho menu "/": mẫu chung + mẫu của Page; mẫu Page 1 không có khi hội thoại thuộc Page 2; q = "xin" khớp
// tiền tố phím tắt; q khớp tiền tố tiêu đề (không phân biệt hoa thường); không pageId: mẫu chung + mẫu của mọi Page
// người gọi đọc được; pageId không đọc được → ErrNotFound; mẫu đã xóa không có.
func TestTemplateList_PageAndPrefix(t *testing.T) {
	t.Parallel()
	e := newTplEnv(t)
	e.mustCreate(e.owner, xin())
	e.mustCreate(e.owner, templates.Input{Shortcut: "camon", Title: "Cảm ơn khách", Body: "Cảm ơn {ten_khach}"})
	e.mustCreate(e.owner, templates.Input{PageID: &e.pageA, Shortcut: "xinsdt", Title: "Xin SĐT", Body: "Cho shop xin SĐT ạ"})
	e.mustCreate(e.owner, templates.Input{PageID: &e.pageB, Shortcut: "xinsize", Title: "Xin size", Body: "Bạn mặc size gì ạ"})
	gone := e.mustCreate(e.owner, templates.Input{Shortcut: "xincu", Title: "Cũ", Body: "Cũ"})
	if err := e.remove(e.owner, gone.ID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		u    identity.AuthUser
		q    string
		page *uuid.UUID
		want []string
	}{
		{"Page B, q xin", e.owner, "xin", &e.pageB, []string{"xindiachi", "xinsize"}},
		{"Page A, q xin", e.reader, "xin", &e.pageA, []string{"xindiachi", "xinsdt"}},
		{"Page A, tiêu đề", e.reader, "cảm", &e.pageA, []string{"camon"}},
		{"Page A, tiêu đề hoa", e.reader, "CẢM ƠN", &e.pageA, []string{"camon"}},
		{"không pageId, chỉ đọc Page A", e.reader, "", nil, []string{"camon", "xindiachi", "xinsdt"}},
		{"không pageId, owner", e.owner, "", nil, []string{"camon", "xindiachi", "xinsdt", "xinsize"}},
	} {
		got, err := e.list(c.u, c.q, c.page)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: %v %v, muốn %v", c.name, got, err, c.want)
		}
	}
	if _, err := e.list(e.reader, "", &e.pageB); !errors.Is(err, templates.ErrNotFound) {
		t.Fatalf("Page không đọc được: %v", err)
	}
}

// reply_templates_v: mẫu đã xóa không có; không có cột body; cột khớp docs/views.md; tenant B không thấy mẫu của A.
func TestReplyTemplatesView(t *testing.T) {
	t.Parallel()
	e := newTplEnv(t)
	keep := e.mustCreate(e.owner, xin())
	gone := e.mustCreate(e.owner, templates.Input{Shortcut: "cu", Title: "Cũ", Body: "Cũ"})
	if err := e.remove(e.owner, gone.ID); err != nil {
		t.Fatal(err)
	}
	var cols []string
	testkit.InTenant(t, e.staff.App, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), `SELECT column_name FROM information_schema.columns
			WHERE table_schema = 'conversation' AND table_name = 'reply_templates_v' ORDER BY ordinal_position`)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		cols, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err //nolint:wrapcheck // test
	})
	if want := []string{"tenant_id", "id", "channel_account_id", "shortcut", "title", "created_at"}; !slices.Equal(cols, want) {
		t.Fatalf("cột view = %v, muốn %v", cols, want)
	}
	var ids []uuid.UUID
	testkit.InTenant(t, e.staff.App, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), `SELECT id FROM conversation.reply_templates_v`)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err //nolint:wrapcheck // test
	})
	if !slices.Equal(ids, []uuid.UUID{keep.ID}) {
		t.Fatalf("view = %v, muốn chỉ %v", ids, keep.ID)
	}
	var n int
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.other, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM conversation.reply_templates_v`).Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("tenant B thấy %d mẫu (%v)", n, err)
	}
}
