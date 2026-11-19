package order_test

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
)

// auditSource là AuditSource của test: đọc audit của đơn bằng identity.ListEntityAudit như lớp HTTP (orderAudit của
// httpapi; order không nhập identity nên bản nối nằm ở mỗi bên gọi).
func auditSource(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]order.AuditChange, error) {
	recs, err := identity.ListEntityAudit(ctx, tx, "order", orderID.String())
	changes := make([]order.AuditChange, len(recs))
	for i, r := range recs {
		actor, perr := uuid.Parse(r.ActorID)
		changes[i] = order.AuditChange{ID: r.ID, Action: r.Action, Fields: r.ChangedFields, Diff: r.HasBefore, At: r.CreatedAt}
		if perr == nil {
			changes[i].ActorID = &actor
		}
	}
	return changes, err //nolint:wrapcheck // test
}

// dbNow đọc giờ của database (clock_timestamp) để mốc của Go và mốc database (audit, ghi chú) cùng một đồng hồ.
func (e *orderEnv) dbNow() time.Time {
	e.t.Helper()
	var now time.Time
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), "SELECT clock_timestamp()").Scan(&now)
	}); err != nil {
		e.t.Fatal(err)
	}
	return now.UTC().Truncate(time.Microsecond)
}

// audit ghi một dòng audit của đơn id như handler (RecordAudit trong tx crm_app).
func (e *orderEnv) audit(user uuid.UUID, action string, id uuid.UUID, before, after map[string]any) {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) error {
		return identity.RecordAudit(e.t.Context(), tx, identity.AuditEntry{
			TenantID: e.tenant, ActorType: "staff", ActorID: user.String(), Action: action, EntityType: "order",
			EntityID: id.String(), Before: before, After: after,
		})
	}); err != nil {
		e.t.Fatalf("audit %s: %v", action, err)
	}
}

func (e *orderEnv) timeline(seller, id uuid.UUID) ([]order.OrderTimelineItem, error) {
	var items []order.OrderTimelineItem
	err := e.app(func(tx pgx.Tx) error {
		var err error
		items, err = order.Timeline(e.t.Context(), tx, seller, id, auditSource)
		return err //nolint:wrapcheck // test
	})
	return items, err
}

func (e *orderEnv) addNote(seller, id uuid.UUID, body string, a order.Actor) (order.Note, error) {
	var n order.Note
	err := e.app(func(tx pgx.Tx) error {
		var err error
		n, err = order.AddNote(e.t.Context(), tx, seller, id, body, a, e.dbNow())
		return err //nolint:wrapcheck // test
	})
	return n, err
}

// DH06: tạo (người 1) → sửa địa chỉ và số lượng (người 2) → xác nhận (người 3) → hủy (người 2) → ghi chú (người 3):
// Timeline có 4 mục thao tác và 1 ghi chú, mới nhất trước, đúng người; mục sửa ghi shipAddress, lines là "changed",
// không có địa chỉ, SĐT cũ hay mới; audit chỉ có after (tạo, xác nhận) và audit chỉ đổi trạng thái không thành mục sửa.
func TestTimeline_DH06(t *testing.T) {
	e := newM2Env(t)
	editor := e.staff.User(t, e.tenant, "sua@elimaz.test", "active")
	confirmer := e.staff.User(t, e.tenant, "xacnhan@elimaz.test", "active")
	v := e.variant(250000, 10)
	in := e.m2Input(0, ln(v, 1, 0))
	in.Intent = order.IntentNew
	o := e.createAt(in, e.dbNow())
	e.audit(e.actor.UserID, "order.create", o.ID, nil, o.AuditFields())

	oldAddr := map[string]any{"name": "Chị Lan", "phone": "0901234567", "line": "12 Lê Lợi"}
	newAddr := map[string]any{"name": "Chị Lan", "phone": "0987654321", "line": "99 Hai Bà Trưng"}
	e.audit(editor, "order.update", o.ID,
		map[string]any{"shipAddress": oldAddr, "lines": []any{map[string]any{"variantId": v.String(), "qty": 1}}, "version": 1},
		map[string]any{"shipAddress": newAddr, "lines": []any{map[string]any{"variantId": v.String(), "qty": 2}}, "version": 2})

	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.writer().Confirm(e.t.Context(), tx, e.seller, o.ID, o.Version,
			order.Actor{TenantID: e.tenant, UserID: confirmer}, e.dbNow())
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	e.audit(confirmer, "order.confirm", o.ID, nil, map[string]any{"status": "confirmed"})
	if err := e.app(func(tx pgx.Tx) error {
		cur, err := order.Get(e.t.Context(), tx, e.seller, o.ID)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		return order.ApplyTransition(e.t.Context(), tx, e.writer(), &cur, order.StatusCancelled, "", "customer_changed_mind",
			order.Actor{TenantID: e.tenant, UserID: editor}, e.dbNow())
	}); err != nil {
		t.Fatalf("hủy: %v", err)
	}
	e.audit(editor, "order.cancel", o.ID, map[string]any{"status": "confirmed", "version": 2},
		map[string]any{"status": "cancelled", "version": 3})
	note, err := e.addNote(e.seller, o.ID, "Khách hẹn gọi lại 0901234567", order.Actor{TenantID: e.tenant, UserID: confirmer})
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}

	items, err := e.timeline(e.seller, o.ID)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	checkTimeline(t, items, []timelineWant{
		{order.TimelineNote, confirmer, map[string]string{}},
		{order.TimelineStatus, editor, map[string]string{"from": "confirmed", "to": "cancelled", "reason": "customer_changed_mind"}},
		{order.TimelineStatus, confirmer, map[string]string{"from": "new", "to": "confirmed"}},
		{order.TimelineEdit, editor, map[string]string{"action": "order.update", "lines": "changed", "shipAddress": "changed"}},
		{order.TimelineStatus, e.actor.UserID, map[string]string{"to": "new"}},
	})
	if items[0].RefID != note.ID || items[0].Note != "Khách hẹn gọi lại 0901234567" || note.AuthorID != confirmer {
		t.Errorf("mục ghi chú: %+v, ghi chú %+v", items[0], note)
	}
	raw, err := json.Marshal(items[1:])
	if err != nil {
		t.Fatal(err)
	}
	for _, pii := range []string{"0901234567", "0987654321", "Lê Lợi", "Hai Bà Trưng", "Chị Lan"} {
		if strings.Contains(string(raw), pii) {
			t.Errorf("lịch sử thao tác lộ %q: %s", pii, raw)
		}
	}

	if _, err := e.timeline(e.partner, o.ID); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("Timeline của người bán khác: %v, muốn ErrNotFound", err)
	}
	if _, err := e.timeline(e.seller, uuid.New()); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("Timeline đơn không có: %v, muốn ErrNotFound", err)
	}
}

// AddNote: thân rỗng hay chỉ khoảng trắng, quá 2.000 ký tự → ErrInvalidInput; đơn của người bán khác hay không có →
// ErrNotFound; thân 2.000 ký tự (có dấu) ghi được, giữ nguyên.
func TestAddNote_Validation(t *testing.T) {
	e := newM2Env(t)
	v := e.variant(100000, 5)
	o := e.createAt(e.m2Input(0, ln(v, 1, 0)), e.dbNow())
	for _, bad := range []string{"", "   \n", strings.Repeat("ă", 2001)} {
		if _, err := e.addNote(e.seller, o.ID, bad, e.actor); !errors.Is(err, order.ErrInvalidInput) {
			t.Errorf("thân %d ký tự: %v, muốn ErrInvalidInput", len([]rune(bad)), err)
		}
	}
	if _, err := e.addNote(e.partner, o.ID, "x", e.actor); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("người bán khác: %v", err)
	}
	if _, err := e.addNote(e.seller, uuid.New(), "x", e.actor); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("đơn không có: %v", err)
	}
	long := strings.Repeat("ă", 2000)
	n, err := e.addNote(e.seller, o.ID, long, e.actor)
	if err != nil || string(n.Body) != long || n.AuthorID != e.actor.UserID || n.OrderID != o.ID {
		t.Errorf("ghi chú 2.000 ký tự: %+v %v", n.ID, err)
	}
	if c := e.count("SELECT count(*) FROM ord.order_notes WHERE order_id = $1", o.ID); c != 1 {
		t.Errorf("số ghi chú: %d", c)
	}
}

type timelineWant struct {
	kind  string
	actor uuid.UUID
	sum   map[string]string
}

// checkTimeline đòi items khớp wants theo thứ tự (loại, người làm, tóm tắt) và mới nhất trước.
func checkTimeline(t *testing.T, items []order.OrderTimelineItem, wants []timelineWant) {
	t.Helper()
	if len(items) != len(wants) {
		t.Fatalf("Timeline có %d mục, muốn %d: %+v", len(items), len(wants), items)
	}
	for i, w := range wants {
		it := items[i]
		if it.Kind != w.kind || it.ActorID == nil || *it.ActorID != w.actor || !maps.Equal(it.Summary, w.sum) {
			t.Errorf("mục %d: %+v, muốn %+v", i, it, w)
		}
		if i > 0 && it.At.After(items[i-1].At) {
			t.Errorf("mục %d (%s) mới hơn mục trước", i, it.At)
		}
	}
}
