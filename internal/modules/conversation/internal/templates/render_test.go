package templates_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/templates"
	"elimaz.com.vn/crm/internal/modules/identity"
)

func (e *tplEnv) render(id uuid.UUID, in templates.RenderInput) (templates.Rendered, error) {
	e.t.Helper()
	var out templates.Rendered
	err := e.tx(func(tx pgx.Tx) error {
		var err error
		out, err = templates.Render(e.t.Context(), tx, id, in)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func missingName(err error) string {
	if m, ok := errors.AsType[*templates.VariableMissingError](err); ok {
		return m.Name
	}
	return ""
}

// CH07, 01 mục 6: "Chào {ten_khach}, …" cho khách Lan → "Chào Lan, …"; khách tên tạm → "Chào bạn, …"; nút trả lời nhanh
// cũng điền biến; tệp đính kèm, nút giữ nguyên.
func TestRender_CustomerName(t *testing.T) {
	t.Parallel()
	e := newTplEnv(t)
	in := xin()
	in.QuickReplies = []channel.QuickReply{{Title: "Gửi {ten_khach}", Payload: "ADDR"}}
	tp := e.mustCreate(e.owner, in)
	got, err := e.render(tp.ID, templates.RenderInput{ConversationID: e.convLan, Actor: &e.reader})
	if err != nil || got.Text != "Chào Lan, cho shop xin địa chỉ nhận hàng ạ" || len(got.QuickReplies) != 1 ||
		got.QuickReplies[0] != (channel.QuickReply{Title: "Gửi Lan", Payload: "ADDR"}) || got.AttachmentFileID != nil {
		t.Fatalf("Lan: %+v %v", got, err)
	}
	got, err = e.render(tp.ID, templates.RenderInput{ConversationID: e.convTmp, Actor: &e.reader})
	if err != nil || got.Text != "Chào bạn, cho shop xin địa chỉ nhận hàng ạ" {
		t.Fatalf("tên tạm: %+v %v", got, err)
	}
}

// {ten_page} là tên Page của hội thoại; {nhan_vien} là tên hiển thị của người gửi, tin tự động (Actor nil) → tên Page.
func TestRender_PageAndStaff(t *testing.T) {
	t.Parallel()
	e := newTplEnv(t)
	tp := e.mustCreate(e.owner, templates.Input{Shortcut: "ky", Title: "Ký tên", Body: "{nhan_vien} - {ten_page}"})
	got, err := e.render(tp.ID, templates.RenderInput{ConversationID: e.convLan, Actor: &e.reader})
	if err != nil || got.Text != "doc-a@elimaz.test - Elimaz Hà Nội" {
		t.Fatalf("nhân viên: %+v %v", got, err)
	}
	got, err = e.render(tp.ID, templates.RenderInput{ConversationID: e.convB})
	if err != nil || got.Text != "Elimaz HCM - Elimaz HCM" {
		t.Fatalf("tự động: %+v %v", got, err)
	}
}

// {ma_don}: khách chưa có đơn → VariableMissingError{ma_don}; không chỉ định → đơn chưa hủy mới nhất của nhóm khách
// (đơn đã hủy mới hơn bị bỏ qua); chỉ định đơn của khách → mã đơn đó; đơn của khách khác hay không có →
// ErrOrderNotOfCustomer. {ma_van_don} chưa có (Task 162) → VariableMissingError{ma_van_don}.
func TestRender_OrderCode(t *testing.T) {
	t.Parallel()
	e := newTplEnv(t)
	tp := e.mustCreate(e.owner, templates.Input{Shortcut: "don", Title: "Đơn", Body: "Đơn {ma_don} đã lên ạ"})
	if _, err := e.render(tp.ID, templates.RenderInput{ConversationID: e.convLan, Actor: &e.reader}); missingName(err) != "ma_don" {
		t.Fatalf("chưa có đơn: %v", err)
	}
	first, firstCode := e.order(e.lan)
	_, latestCode := e.order(e.lan)
	cancelled, _ := e.order(e.lan)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE ord.orders SET status = 'cancelled', cancelled_at = now(),
			cancel_reason = 'customer_changed_mind' WHERE id = $1`, cancelled)
		return err //nolint:wrapcheck // test
	})
	maiOrder, _ := e.order(e.mai)
	got, err := e.render(tp.ID, templates.RenderInput{ConversationID: e.convLan, Actor: &e.reader})
	if err != nil || got.Text != "Đơn "+latestCode+" đã lên ạ" {
		t.Fatalf("mới nhất: %+v %v (muốn %s)", got, err, latestCode)
	}
	got, err = e.render(tp.ID, templates.RenderInput{ConversationID: e.convLan, OrderID: &first, Actor: &e.reader})
	if err != nil || got.Text != "Đơn "+firstCode+" đã lên ạ" {
		t.Fatalf("chỉ định: %+v %v", got, err)
	}
	for _, o := range []uuid.UUID{maiOrder, uuid.New()} {
		if _, err := e.render(tp.ID, templates.RenderInput{ConversationID: e.convLan, OrderID: &o, Actor: &e.reader}); !errors.Is(err, templates.ErrOrderNotOfCustomer) {
			t.Fatalf("đơn %s: %v", o, err)
		}
	}
	vd := e.mustCreate(e.owner, templates.Input{Shortcut: "vd", Title: "Vận đơn", Body: "Mã vận đơn {ma_van_don}"})
	if _, err := e.render(vd.ID, templates.RenderInput{ConversationID: e.convLan, Actor: &e.reader}); missingName(err) != "ma_van_don" {
		t.Fatalf("mã vận đơn: %v", err)
	}
}

// Quyền và phạm vi: hội thoại trên Page người gọi không đọc được → ErrNotFound; mẫu của Page A cho hội thoại Page B →
// ErrNotFound; mẫu đã xóa, hội thoại không có → ErrNotFound.
func TestRender_AccessAndScope(t *testing.T) {
	t.Parallel()
	e := newTplEnv(t)
	common := e.mustCreate(e.owner, xin())
	pageA := e.mustCreate(e.owner, templates.Input{PageID: &e.pageA, Shortcut: "a", Title: "A", Body: "Chỉ Page A"})
	for _, c := range []struct {
		name  string
		tpl   uuid.UUID
		conv  uuid.UUID
		actor *identity.AuthUser
	}{
		{"Page B không có quyền", common.ID, e.convB, &e.reader},
		{"mẫu Page A ở hội thoại Page B", pageA.ID, e.convB, &e.readerB},
		{"hội thoại không có", common.ID, uuid.New(), &e.owner},
		{"mẫu không có", uuid.New(), e.convLan, &e.owner},
	} {
		if _, err := e.render(c.tpl, templates.RenderInput{ConversationID: c.conv, Actor: c.actor}); !errors.Is(err, templates.ErrNotFound) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if got, err := e.render(pageA.ID, templates.RenderInput{ConversationID: e.convLan, Actor: &e.reader}); err != nil || got.Text != "Chỉ Page A" {
		t.Fatalf("mẫu Page A ở Page A: %+v %v", got, err)
	}
}

var knownVar = regexp.MustCompile(`\{(ten_khach|ten_page|ma_don|nhan_vien|ma_van_don)\}`)

// Quy luật của Fill: chuỗi không có biến đã biết giữ nguyên; kết quả điền đủ giá trị không còn biến đã biết (giá trị
// không chứa "{"); Variables trả đúng tập biến đã biết của chuỗi.
func TestFill_Properties(t *testing.T) {
	t.Parallel()
	piece := rapid.SampledFrom([]string{
		"Chào ", "{ten_khach}", "{ten_page}", "{ma_don}", "{nhan_vien}", "{", "}", "{}", "{x y}", "ạ", ", ", "{{ten_khach}}",
	})
	rapid.Check(t, func(rt *rapid.T) {
		s := strings.Join(rapid.SliceOf(piece).Draw(rt, "pieces"), "")
		val := rapid.StringMatching(`[A-Za-zÀ-ỹ0-9 ]{0,10}`).Draw(rt, "val")
		out, err := templates.Fill(s, func(string) (string, error) { return val, nil })
		if err != nil {
			rt.Fatalf("Fill(%q): %v", s, err)
		}
		if !knownVar.MatchString(s) && out != s {
			rt.Fatalf("không có biến mà đổi: %q → %q", s, out)
		}
		if knownVar.MatchString(out) {
			rt.Fatalf("còn biến: %q → %q", s, out)
		}
		names, err := templates.Variables(s)
		if err != nil {
			rt.Fatalf("Variables(%q): %v", s, err)
		}
		if (len(names) > 0) != knownVar.MatchString(s) {
			rt.Fatalf("Variables(%q) = %v", s, names)
		}
	})
	if _, err := templates.Variables("Chào {abc}"); !errors.Is(err, templates.ErrUnknownVariable) {
		t.Fatalf("biến lạ: %v", err)
	}
}
