package conversation_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/conversation/convtest"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/order"
)

func validSettings(src uuid.UUID, users ...uuid.UUID) conversation.PageSettingsInput {
	return conversation.PageSettingsInput{
		RoundRobinEnabled: true, RoundRobinUserIDs: users, InboxTrafficSourceID: &src,
		LiveSlots: []conversation.LiveSlot{
			{From: "11:00", To: "14:00", TrafficSourceID: src}, {From: "14:00", To: "15:30", TrafficSourceID: src},
		},
		CommentPollMode: "auto", AutomationMaxAgeMinutes: 30,
	}
}

// Cài đặt Page: chưa lưu → mặc định version 0; hai khung giờ chồng nhau, nguồn đơn lạ/đã tắt, người xoay vòng không có
// quyền trên Page → lỗi; hợp lệ → version 1 rồi 2, audit, báo hiệu; version cũ →
// ErrVersionConflict; sale chỉ đọc → ErrNotFound; Page không có → channel.ErrAccountNotFound.
func TestPageSettings(t *testing.T) {
	t.Parallel()
	e := newTagEnv(t)
	src := e.s.TrafficSource(t, e.tenant, "Hộp thư", true)
	off := e.s.TrafficSource(t, e.tenant, "Đã tắt", false)
	u1 := convtest.Staff(t, e.s, e.tenant, e.page1, "u1@elimaz.test", "conversation.read")
	outsider := convtest.Staff(t, e.s, e.tenant, e.page2, "u2@elimaz.test", "conversation.read")
	get := func(email string, page uuid.UUID) (conversation.PageSettings, error) {
		u := convtest.Load(t, e.s, email)
		var out conversation.PageSettings
		err := e.write(func(tx pgx.Tx, _ conversation.Writer) (err error) {
			out, err = conversation.GetPageSettings(t.Context(), tx, u, page)
			return err //nolint:wrapcheck // test
		})
		return out, err
	}
	put := func(in conversation.PageSettingsInput, v int32) (conversation.PageSettings, error) {
		return e.putAs(chatManager, e.page1, in, v)
	}
	s, err := get(chatSale, e.page1)
	if err != nil || s.Version != 0 || s.RoundRobinEnabled || s.CommentPollMode != "auto" || s.AutomationMaxAgeMinutes != 15 ||
		len(s.LiveSlots) != 0 {
		t.Fatalf("mặc định: %+v %v", s, err)
	}
	if _, err := get(chatSale, e.page2); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("sale đọc Page 2: %v", err)
	}
	if _, err := get(chatManager, uuid.New()); !errors.Is(err, channel.ErrAccountNotFound) {
		t.Fatalf("Page không có: %v", err)
	}
	overlap := validSettings(src, u1)
	overlap.LiveSlots[1].From = "13:00"
	reversed := validSettings(src, u1)
	reversed.LiveSlots[0].To = "10:00"
	badSource := validSettings(src, u1)
	badSource.CommentTrafficSourceID = &off
	unknown := validSettings(uuid.New(), u1)
	for _, c := range []struct {
		name string
		in   conversation.PageSettingsInput
		want error
	}{
		{"hai khung chồng nhau", overlap, conversation.ErrInvalidInput},
		{"khung ngược", reversed, conversation.ErrInvalidInput},
		{"nguồn đã tắt", badSource, order.ErrTrafficSourceInactive},
		{"nguồn lạ", unknown, order.ErrTrafficSourceInactive},
		{"người xoay vòng không có quyền trên Page", validSettings(src, outsider), conversation.ErrInvalidInput},
	} {
		if _, err := put(c.in, 0); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, muốn %v", c.name, err, c.want)
		}
	}
	got, err := put(validSettings(src, u1), 0)
	if err != nil || got.Version != 1 || !got.RoundRobinEnabled || len(got.LiveSlots) != 2 || *got.InboxTrafficSourceID != src {
		t.Fatalf("lưu lần đầu: %+v %v", got, err)
	}
	if _, err := put(validSettings(src, u1), 0); !errors.Is(err, conversation.ErrVersionConflict) {
		t.Fatalf("version cũ: %v", err)
	}
	next := validSettings(src)
	next.RoundRobinEnabled = false
	if got, err = put(next, 1); err != nil || got.Version != 2 || got.RoundRobinEnabled || len(got.RoundRobinUserIDs) != 0 {
		t.Fatalf("lưu lần hai: %+v %v", got, err)
	}
	if n := e.s.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs
		WHERE action = 'conversation.page_settings_updated' AND entity_id = $1`, e.page1.String()); n != 2 {
		t.Fatalf("audit = %d", n)
	}
	if sig := e.bus.Signals(); len(sig) != 2 || sig[0].Entity.Type != conversation.EntityPageSettings ||
		sig[0].Scope == nil || sig[0].Scope.ID != e.page1.String() {
		t.Fatalf("báo hiệu = %+v", sig)
	}
	if _, err := e.putAs(chatSale, e.page1, next, 2); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("sale lưu cài đặt: %v", err)
	}
}

func (e *tagEnv) putAs(email string, page uuid.UUID, in conversation.PageSettingsInput, v int32) (conversation.PageSettings, error) {
	u := convtest.Load(e.t, e.s, email)
	var out conversation.PageSettings
	err := e.write(func(tx pgx.Tx, w conversation.Writer) (err error) {
		out, err = w.UpdatePageSettings(e.t.Context(), tx, u, page, in, v)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

// Mẫu xin địa chỉ (Task 161 thay chốt "chưa có bảng mẫu" của Task 160): mẫu không có, đã xóa, riêng của Page khác →
// ErrInvalidInput; mẫu chung và mẫu riêng của đúng Page → lưu được, đọc lại đúng id; bỏ mẫu (nil) → lưu được.
func TestPageSettings_AskAddressTemplate(t *testing.T) {
	t.Parallel()
	e := newTagEnv(t)
	src := e.s.TrafficSource(t, e.tenant, "Hộp thư", true)
	shared := e.template(nil, "xin-dia-chi")
	own := e.template(&e.page1, "dc-page1")
	other := e.template(&e.page2, "dc-page2")
	gone := e.template(nil, "da-xoa")
	e.deleteTemplate(gone)
	with := func(id *uuid.UUID) conversation.PageSettingsInput {
		in := validSettings(src)
		in.AskAddressTemplateID = id
		return in
	}
	for name, id := range map[string]uuid.UUID{"không có": uuid.New(), "đã xóa": gone, "của Page khác": other} {
		if _, err := e.putAs(chatManager, e.page1, with(&id), 0); !errors.Is(err, conversation.ErrInvalidInput) {
			t.Errorf("mẫu %s: %v, muốn ErrInvalidInput", name, err)
		}
	}
	for v, id := range []*uuid.UUID{&shared, &own, nil} {
		got, err := e.putAs(chatManager, e.page1, with(id), int32(v))
		if err != nil || got.Version != int32(v)+1 || (id == nil) != (got.AskAddressTemplateID == nil) ||
			(id != nil && *got.AskAddressTemplateID != *id) {
			t.Fatalf("lưu mẫu %v (lần %d): %+v %v", id, v+1, got, err)
		}
	}
}

// template tạo mẫu trả lời (page nil = mẫu chung) bằng quản lý toàn shop, trả id.
func (e *tagEnv) template(page *uuid.UUID, shortcut string) uuid.UUID {
	e.t.Helper()
	u := convtest.Load(e.t, e.s, chatManager)
	var id uuid.UUID
	if err := e.write(func(tx pgx.Tx, _ conversation.Writer) error {
		tp, err := conversation.TemplateWriter{}.Create(e.t.Context(), tx, u, conversation.TemplateInput{
			PageID: page, Shortcut: shortcut, Title: "Xin địa chỉ", Body: "Chị cho em xin địa chỉ nhận hàng ạ",
		})
		id = tp.ID
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("tạo mẫu %s: %v", shortcut, err)
	}
	return id
}

func (e *tagEnv) deleteTemplate(id uuid.UUID) {
	e.t.Helper()
	u := convtest.Load(e.t, e.s, chatManager)
	if err := e.write(func(tx pgx.Tx, _ conversation.Writer) error {
		return conversation.TemplateWriter{}.Delete(e.t.Context(), tx, u, id)
	}); err != nil {
		e.t.Fatalf("xóa mẫu: %v", err)
	}
}

// Hằng của phần assign trùng hằng công khai của gói gốc.
func TestAssignConstants(t *testing.T) {
	t.Parallel()
	if conversation.TopicConversationChanged != assign.TopicChanged ||
		conversation.EventConversationAssigned != assign.EventAssigned {
		t.Fatal("hằng lệch")
	}
}
