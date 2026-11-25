package httpapi

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	chOwner    = "chu-ch@elimaz.test"
	chReaderA  = "sale-a@elimaz.test"
	chReaderNo = "sale-khac@elimaz.test"
	chManager  = "doc-het@elimaz.test"
)

type chAPIEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	keys   *cfaccesstest.AccessKeys
	srv    http.Handler
	bus    *realtimetest.RecordingBus
	tenant uuid.UUID
	pageA  uuid.UUID
	pageB  uuid.UUID
}

func newChAPIEnv(t *testing.T) *chAPIEnv {
	t.Helper()
	e := &chAPIEnv{t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t), bus: &realtimetest.RecordingBus{}}
	e.tenant = e.staff.Tenant(t, "A")
	e.pageA = e.staff.ChannelAccount(t, e.tenant, "fakechat", "page-a", "Page A")
	e.pageB = e.staff.ChannelAccount(t, e.tenant, "facebook", "page-b", "Page B")
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, chOwner, "active"), rolePerms(t, "owner"), "all", "")
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, chReaderA, "active"), []string{"conversation.read", "conversation.reply"},
		"page", e.pageA.String())
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, chReaderNo, "active"), []string{"conversation.read"}, "page", uuid.NewString())
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, chManager, "active"), []string{"conversation.read"}, "all", "")
	kr, err := fieldcrypt.ParseKeyring(logging.Secret("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return channel.StoreCredentials(t.Context(), tx, kr, e.pageA, "EAAB-secret-77", channel.TokenPageUser)
	})
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Realtime = e.bus
	e.srv = NewServer(d)
	return e
}

func (e *chAPIEnv) list(email string) (int, []any, string) {
	e.t.Helper()
	rec := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/channels", email, "", "")
	if rec.Code != http.StatusOK {
		return rec.Code, nil, rec.Body.String()
	}
	items, _ := decodeJSON(e.t, rec)["items"].([]any)
	return rec.Code, items, rec.Body.String()
}

// PQ04: danh sách Page theo phạm vi conversation.read; không bao giờ có token.
func TestChannelAPI_ListByScope(t *testing.T) {
	t.Parallel()
	e := newChAPIEnv(t)
	for _, c := range []struct {
		email string
		want  int
	}{{chOwner, 2}, {chManager, 2}, {chReaderA, 1}, {chReaderNo, 0}} {
		code, items, body := e.list(c.email)
		if code != http.StatusOK || len(items) != c.want {
			t.Fatalf("%s: %d, %d dòng; muốn %d: %s", c.email, code, len(items), c.want, body)
		}
		for _, bad := range []string{"credential", "token\"", "EAAB", "accessToken"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s: câu trả lời chứa %q: %s", c.email, bad, body)
			}
		}
	}
	_, items, _ := e.list(chReaderA)
	a, _ := items[0].(map[string]any)
	if a["id"] != e.pageA.String() || a["sendMode"] != "read_only" || a["status"] != "active" || a["version"] != float64(1) ||
		a["channel"] != "fakechat" || a["externalId"] != "page-a" {
		t.Fatalf("Page A = %v", a)
	}
}

// Đổi chế độ gửi: thiếu channel.manage → 403; owner → 200 version+1, một audit {send_mode}, một lệnh cảnh báo, một báo
// hiệu channel.status; gửi lại cùng khóa → Idempotent-Replayed, không thêm gì; version cũ → 409; Page không có → 404.
func TestChannelAPI_SetSendMode(t *testing.T) {
	t.Parallel()
	e := newChAPIEnv(t)
	path := "/api/v1/channels/" + e.pageA.String() + "/send-mode"
	body := `{"mode":"enabled","expectedVersion":1}`
	expect(t, "sale không có channel.manage", serveAs(e.srv, e.keys, http.MethodPut, path, chReaderA, "k0", body),
		http.StatusForbidden, "forbidden")
	rec := serveAs(e.srv, e.keys, http.MethodPut, path, chOwner, "k1", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeJSON(t, rec); got["sendMode"] != "enabled" || got["version"] != float64(2) {
		t.Fatalf("kết quả = %v", got)
	}
	again := serveAs(e.srv, e.keys, http.MethodPut, path, chOwner, "k1", body)
	if again.Code != http.StatusOK || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("gửi lại cùng khóa: %d %v", again.Code, again.Header())
	}
	expect(t, "version cũ", serveAs(e.srv, e.keys, http.MethodPut, path, chOwner, "k2", body), http.StatusConflict, "version_conflict")
	expect(t, "Page không có", serveAs(e.srv, e.keys, http.MethodPut, "/api/v1/channels/"+uuid.NewString()+"/send-mode",
		chOwner, "k3", body), http.StatusNotFound, "not_found")
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'channel.send_mode_changed'
		AND entity_id = $1 AND changed_fields = '{send_mode}'`, e.pageA.String()); n != 1 {
		t.Fatalf("audit = %d", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = $1`, channel.SendModeAlertKind); n != 1 {
		t.Fatalf("lệnh cảnh báo = %d", n)
	}
	sig := e.bus.Signals()
	if len(sig) != 1 || sig[0].Topic != "channel.status" || sig[0].Scope == nil || sig[0].Scope.ID != e.pageA.String() {
		t.Fatalf("báo hiệu = %+v", sig)
	}
	_, items, _ := e.list(chReaderA)
	if a, _ := items[0].(map[string]any); a["sendMode"] != "enabled" || a["version"] != float64(2) {
		t.Fatalf("sau khi đổi = %v", a)
	}
}

// Thẻ nhân viên: owner bật → 200, version+1, không lệnh cảnh báo; thiếu quyền → 403.
func TestChannelAPI_SetHumanAgent(t *testing.T) {
	t.Parallel()
	e := newChAPIEnv(t)
	path := "/api/v1/channels/" + e.pageB.String() + "/human-agent"
	body := `{"enabled":true,"expectedVersion":1}`
	expect(t, "thiếu quyền", serveAs(e.srv, e.keys, http.MethodPut, path, chManager, "h0", body), http.StatusForbidden, "forbidden")
	rec := serveAs(e.srv, e.keys, http.MethodPut, path, chOwner, "h1", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if got := decodeJSON(t, rec); got["humanAgentEnabled"] != true || got["version"] != float64(2) {
		t.Fatalf("kết quả = %v", got)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = $1`, channel.SendModeAlertKind); n != 0 {
		t.Fatalf("lệnh cảnh báo = %d", n)
	}
}
