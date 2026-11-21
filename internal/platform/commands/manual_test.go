package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// resolveManually gọi commands.ResolveManually trong ngữ cảnh tenant của e (hoặc tenant khác).
func resolveManually(t *testing.T, e *env, tenant, id uuid.UUID, result json.RawMessage, hs ...commands.Handler) error {
	t.Helper()
	return tenancy.WithTenant(t.Context(), e.app, tenant, func(tx pgx.Tx) error { //nolint:wrapcheck // test
		return commands.ResolveManually(t.Context(), tx, id, result, hs)
	})
}

// ResolveManually (Task M3-12): lệnh in_doubt hay dead → done kèm result, OnDone được gọi một lần với đúng result
// trong ngữ cảnh tenant, không gửi lại ra ngoài; gọi lần hai → ErrCommandNotResolvable, OnDone không chạy thêm. Lệnh
// pending, đang chạy, đã expired → ErrCommandNotResolvable, không đổi; tenant khác, không có → ErrCommandNotFound; loại
// lệnh không có trong hs → ErrCommandNotResolvable.
func TestResolveManually(t *testing.T) {
	e := newEnv(t)
	h := &lookH{cmdH: newCmd("command.look", &testkit.FakeExternal{})}
	res := json.RawMessage(`{"trackingCode":"FK7777"}`)

	doubt := inDoubtJob(t, e, h, "m1", true)
	calls := len(h.ext.Calls())
	if err := resolveManually(t, e, e.tenant, doubt, res, h); err != nil {
		t.Fatalf("ResolveManually in_doubt: %v", err)
	}
	row := e.job(t, doubt)
	if row.Status != "done" || string(row.Result) != `{"trackingCode": "FK7777"}` || row.LastError != nil {
		t.Fatalf("muốn done kèm result: %+v %s", row, row.Result)
	}
	_, dones, _ := h.snapshot()
	if len(dones) != 1 || dones[0] != doubt.String() || !h.tenantOK || len(h.ext.Calls()) != calls {
		t.Fatalf("OnDone %v (tenant %v), gọi ngoài %d → %d", dones, h.tenantOK, calls, len(h.ext.Calls()))
	}
	if err := resolveManually(t, e, e.tenant, doubt, res, h); !errors.Is(err, commands.ErrCommandNotResolvable) {
		t.Fatalf("gọi lần hai: %v, muốn ErrCommandNotResolvable", err)
	}
	if _, dones, _ := h.snapshot(); len(dones) != 1 {
		t.Fatalf("OnDone chạy thêm: %v", dones)
	}

	dead := e.enqueue(t, h.kind, "m2")
	e.ownerExec(t, "UPDATE platform.jobs SET status = 'dead', finished_at = now(), last_error = 'confirmed_not_sent' WHERE id = $1", dead)
	if err := resolveManually(t, e, e.tenant, dead, res, h); err != nil {
		t.Fatalf("ResolveManually dead: %v", err)
	}
	if st := e.job(t, dead).Status; st != "done" {
		t.Fatalf("lệnh dead = %s, muốn done", st)
	}

	pending := e.enqueue(t, h.kind, "m3")
	running := e.enqueue(t, h.kind, "m4")
	e.ownerExec(t, `UPDATE platform.jobs SET status = 'running', attempts = 1, locked_by = 'w',
		lease_until = now() + interval '1 minute' WHERE id = $1`, running)
	expired := e.enqueue(t, h.kind, "m5")
	e.ownerExec(t, "UPDATE platform.jobs SET status = 'expired', finished_at = now() WHERE id = $1", expired)
	for name, id := range map[string]uuid.UUID{"pending": pending, "running": running, "expired": expired} {
		before := e.job(t, id).Status
		if err := resolveManually(t, e, e.tenant, id, res, h); !errors.Is(err, commands.ErrCommandNotResolvable) {
			t.Fatalf("%s: %v, muốn ErrCommandNotResolvable", name, err)
		}
		if st := e.job(t, id).Status; st != before {
			t.Fatalf("%s bị đổi: %s → %s", name, before, st)
		}
	}

	other := inDoubtJob(t, e, h, "m6", true)
	var tenantB uuid.UUID
	if err := e.owner.Pool.QueryRow(t.Context(), "INSERT INTO platform.tenants (name) VALUES ('B') RETURNING id").Scan(&tenantB); err != nil {
		t.Fatal(err)
	}
	if err := resolveManually(t, e, tenantB, other, res, h); !errors.Is(err, commands.ErrCommandNotFound) {
		t.Fatalf("tenant khác: %v, muốn ErrCommandNotFound", err)
	}
	if err := resolveManually(t, e, e.tenant, uuid.New(), res, h); !errors.Is(err, commands.ErrCommandNotFound) {
		t.Fatalf("không có lệnh: %v, muốn ErrCommandNotFound", err)
	}
	if err := resolveManually(t, e, e.tenant, other, res); !errors.Is(err, commands.ErrCommandNotResolvable) {
		t.Fatalf("không có handler: %v, muốn ErrCommandNotResolvable", err)
	}
	if st := e.job(t, other).Status; st != "in_doubt" {
		t.Fatalf("lệnh không có handler bị đổi: %s", st)
	}
}

// OnDone trả lỗi → lệnh giữ in_doubt (transaction của bên gọi lùi), lỗi trả lên.
func TestResolveManually_HookFails(t *testing.T) {
	e := newEnv(t)
	h := &manualFailDone{lookH: &lookH{cmdH: newCmd("command.look", &testkit.FakeExternal{})}}
	id := inDoubtJob(t, e, h.lookH, "f1", true)
	if err := resolveManually(t, e, e.tenant, id, json.RawMessage(`{}`), h); !errors.Is(err, errManualHook) {
		t.Fatalf("hook lỗi: %v, muốn errManualHook", err)
	}
	if st := e.job(t, id).Status; st != "in_doubt" {
		t.Fatalf("lệnh = %s, muốn in_doubt", st)
	}
}

var errManualHook = errors.New("hook lỗi")

type manualFailDone struct{ *lookH }

func (*manualFailDone) OnDone(context.Context, queue.Job, json.RawMessage, pgx.Tx) error {
	return errManualHook
}
