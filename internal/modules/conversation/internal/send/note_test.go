package send_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// noteEnv: tenant có hội thoại conv trên Page page (mã ngẫu nhiên: messages không có khóa ngoại tới channel_accounts),
// nhân viên sale (đọc, trả lời Page), viewer (chỉ đọc Page).
type noteEnv struct {
	t            *testing.T
	staff        *testkit.Staff
	tenant, page uuid.UUID
	conv         uuid.UUID
	sale, viewer identity.AuthUser
	bus          *realtimetest.RecordingBus
}

func newNoteEnv(t *testing.T) *noteEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &noteEnv{t: t, staff: s, tenant: s.Tenant(t, "A"), page: uuid.New(), bus: &realtimetest.RecordingBus{}}
	s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `WITH k AS (
				INSERT INTO customer.customers (tenant_id, full_name, name_search)
				VALUES (current_setting('app.tenant_id')::uuid, 'Hoa', 'hoa') RETURNING tenant_id, id)
			INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind, external_thread_ref,
				last_message_at)
			SELECT tenant_id, $1, id, 'inbox', 'psid-note', now() FROM k RETURNING id`, e.page).Scan(&e.conv)
	})
	e.sale = e.user("sale@elimaz.test", "conversation.read", "conversation.reply")
	e.viewer = e.user("xem@elimaz.test", "conversation.read")
	return e
}

func (e *noteEnv) user(email string, perms ...string) identity.AuthUser {
	e.t.Helper()
	id := e.staff.User(e.t, e.tenant, email, "active")
	e.staff.Grant(e.t, e.tenant, id, perms, identity.ScopePage, e.page.String())
	u := identity.AuthUser{UserID: id, TenantID: e.tenant, Email: email, Kind: identity.KindStaff}
	for _, p := range perms {
		u.Grants = append(u.Grants, identity.Grant{
			Permission: identity.Permission(p), Scope: identity.Scope{Type: identity.ScopePage, ID: e.page.String()},
		})
	}
	return u
}

func (e *noteEnv) add(u identity.AuthUser, conv uuid.UUID, text string) (send.Note, error) {
	e.t.Helper()
	var n send.Note
	err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		n, err = send.AddNote(e.t.Context(), tx, e.bus, u, conv, text, time.Date(2026, 11, 26, 3, 0, 0, 0, time.UTC))
		return err //nolint:wrapcheck // test
	})
	return n, err //nolint:wrapcheck // test
}

// CH07: ghi chú → đúng 1 tin note, out, staff, không trạng thái gửi, không mã ngoài, không lệnh; số dòng jobs trước và
// sau bằng nhau (không gì đi ra Meta); một báo hiệu conversation.changed theo Page, không có nội dung.
func TestNote_NeverLeaves(t *testing.T) {
	t.Parallel()
	e := newNoteEnv(t)
	jobs := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs`)
	n, err := e.add(e.sale, e.conv, "  Khách hẹn chiều mai chuyển khoản  ")
	if err != nil {
		t.Fatal(err)
	}
	if n.Body != "Khách hẹn chiều mai chuyển khoản" || n.SenderID != e.sale.UserID || n.SenderName != "sale@elimaz.test" ||
		n.ConversationID != e.conv || n.ID == uuid.Nil {
		t.Fatalf("ghi chú = %+v", n)
	}
	if c := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE conversation_id = $1 AND kind = 'note'
		AND direction = 'out' AND origin = 'staff' AND send_status IS NULL AND external_id IS NULL AND command_job_id IS NULL
		AND sender_staff_id = $2`, e.conv, e.sale.UserID); c != 1 {
		t.Fatalf("tin note = %d", c)
	}
	if after := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs`); after != jobs {
		t.Fatalf("jobs %d → %d", jobs, after)
	}
	sig := e.bus.Signals()
	if len(sig) != 1 || sig[0].Topic != "conversation.changed" || sig[0].Scope == nil || sig[0].Scope.ID != e.page.String() ||
		sig[0].Entity.ID != e.conv.String() {
		t.Fatalf("báo hiệu = %+v", sig)
	}
}

// Database giữ quy luật: UPDATE … SET send_status = 'queued' (hay external_id) trên ghi chú → vi phạm CHECK
// messages_note_internal (23514), kể cả với chủ bảng.
func TestNote_CheckBlocksSending(t *testing.T) {
	t.Parallel()
	e := newNoteEnv(t)
	n, err := e.add(e.sale, e.conv, "Ghi chú")
	if err != nil {
		t.Fatal(err)
	}
	for _, set := range []string{"send_status = 'queued'", "external_id = 'm_1'", "command_job_id = gen_random_uuid()"} {
		err := e.staff.TryExec(t.Context(), e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), `UPDATE conversation.messages SET `+set+` WHERE id = $1`, n.ID)
			return err //nolint:wrapcheck // test
		})
		if pe, ok := errors.AsType[*pgconn.PgError](err); !ok || pe.Code != "23514" || pe.ConstraintName != "messages_note_internal" {
			t.Errorf("%s: %v", set, err)
		}
	}
}

// Quyền và dữ liệu: chỉ đọc (không conversation.reply) hay hội thoại không có → ErrNotFound; thân rỗng sau khi bỏ khoảng
// trắng, quá 4.000 ký tự → ErrInvalidNote; đúng 4.000 ký tự (có dấu) được.
func TestNote_AccessAndLength(t *testing.T) {
	t.Parallel()
	e := newNoteEnv(t)
	for _, c := range []struct {
		name string
		u    identity.AuthUser
		conv uuid.UUID
		text string
		want error
	}{
		{"chỉ đọc", e.viewer, e.conv, "x", send.ErrNotFound},
		{"hội thoại không có", e.sale, uuid.New(), "x", send.ErrNotFound},
		{"rỗng", e.sale, e.conv, " \n\t ", send.ErrInvalidNote},
		{"4001 ký tự", e.sale, e.conv, strings.Repeat("ạ", 4001), send.ErrInvalidNote},
	} {
		if _, err := e.add(c.u, c.conv, c.text); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, muốn %v", c.name, err, c.want)
		}
	}
	if _, err := e.add(e.sale, e.conv, strings.Repeat("ạ", 4000)); err != nil {
		t.Fatalf("4000 ký tự: %v", err)
	}
	if len(e.bus.Signals()) != 1 {
		t.Fatalf("báo hiệu = %d, muốn 1 (lần bị từ chối không báo)", len(e.bus.Signals()))
	}
}
