package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// newSendAPIEnv là chatAPIEnv có bộ adapter kênh (Facebook, chỉ để hỏi SendPolicy, không gọi mạng), Page A bật gửi
// thật, tin cuối của khách trên hội thoại convA cách now 1 giờ.
func newSendAPIEnv(t *testing.T) *chatAPIEnv {
	t.Helper()
	e := newChatAPIEnv(t)
	reg, err := channel.NewRegistry(facebook.New(facebook.Options{AppSecret: logging.Secret(strings.Repeat("a", 32))}))
	if err != nil {
		t.Fatal(err)
	}
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Realtime, d.Now, d.Channels = &realtimetest.RecordingBus{}, func() time.Time { return e.now }, reg
	e.srv = NewServer(d)
	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'enabled'`)
	e.exec(`UPDATE conversation.conversations SET last_customer_message_at = $1 WHERE id = $2`, e.now.Add(-time.Hour), e.convA)
	return e
}

// exec sửa dữ liệu thử (chủ bảng, tenant A) bằng một câu sql.
func (e *chatAPIEnv) exec(sql string, args ...any) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, sqlStep(e.t.Context(), sql, args))
}

// sqlStep là bước chạy một câu sql trong tx của bên gọi.
func sqlStep(ctx context.Context, sql string, args []any) func(pgx.Tx) error {
	return func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			return fmt.Errorf("chạy câu dữ liệu thử: %w", err)
		}
		return nil
	}
}

// sendJobs đếm lệnh gửi tin (platform.jobs không có RLS).
func (e *chatAPIEnv) sendJobs() int {
	e.t.Helper()
	var n int
	if err := e.staff.Owner.Pool.QueryRow(e.t.Context(), `SELECT count(*) FROM platform.jobs WHERE kind = $1`,
		conversation.SendMessageKind).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// CH03 qua HTTP: 201 tin queued kèm đúng một lệnh gửi; cùng Idempotency-Key gửi 50 lần song song → 1 tin, 1 lệnh, mọi
// câu trả lời là 201 cùng thân hoặc 409 request_in_progress.
func TestSendMessageAPI_CH03(t *testing.T) {
	t.Parallel()
	e := newSendAPIEnv(t)
	path := "/api/v1/conversations/" + e.convA.String() + "/messages"
	got := mustOK(t, "sendMessage", e.send(http.MethodPost, path, chatSaleA, `{"text":"Dạ shop chào chị"}`), http.StatusCreated)
	if got["sendStatus"] != "queued" || got["body"] != "Dạ shop chào chị" || got["origin"] != "staff" ||
		got["direction"] != "out" || got["conversationId"] != e.convA.String() {
		t.Fatalf("tin = %v", got)
	}
	if n := e.sendJobs(); n != 1 {
		t.Fatalf("%d lệnh gửi, muốn 1", n)
	}

	key := "same-" + uuid.NewString()
	var wg sync.WaitGroup
	var mu sync.Mutex
	bodies := map[string]int{}
	for range 50 {
		wg.Go(func() {
			rec := serveAs(e.srv, e.keys, http.MethodPost, path, chatSaleA, key, `{"text":"Bấm hai lần"}`)
			mu.Lock()
			defer mu.Unlock()
			switch rec.Code {
			case http.StatusCreated:
				bodies[rec.Body.String()]++
			case http.StatusConflict:
				if !strings.Contains(rec.Body.String(), "request_in_progress") {
					t.Errorf("409 lạ: %s", rec.Body.String())
				}
			default:
				t.Errorf("mã %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
	wg.Wait()
	if len(bodies) != 1 {
		t.Fatalf("201 với %d thân khác nhau, muốn 1", len(bodies))
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE body = 'Bấm hai lần'`); n != 1 {
		t.Fatalf("%d tin, muốn 1", n)
	}
	if n := e.sendJobs(); n != 2 {
		t.Fatalf("%d lệnh gửi, muốn 2", n)
	}
}

// Ca từ chối qua HTTP, không ghi tin nào: cả chữ lẫn ảnh → 400; không quyền trên Page B → 404; tin cuối của khách
// cách 25 giờ → 409 send_not_allowed kèm denyReason window_closed (không until: không còn đường gửi); Page bật Human
// Agent, cách 3 ngày, thiếu humanAgent → 409 window_closed kèm until = hạn 7 ngày; Page chỉ xem → 409 channel_read_only; ảnh
// đang xử lý → 422 attachment_not_ready; ảnh sản phẩm → 422 attachment_rejected; ảnh không có → 404.
func TestSendMessageAPI_Refusals(t *testing.T) {
	t.Parallel()
	e := newSendAPIEnv(t)
	path := "/api/v1/conversations/" + e.convA.String() + "/messages"
	processing, product := uuid.New(), uuid.New()
	e.exec(`INSERT INTO platform.files (id, tenant_id, purpose, bucket, key, original_name, declared_type, size_bytes, status)
		VALUES ($1::uuid, current_setting('app.tenant_id')::uuid, $3, 'originals', 'k-' || $1::text, 'a.jpg', 'image/jpeg', 1, 'processing'),
		       ($2::uuid, current_setting('app.tenant_id')::uuid, 'product_image', 'originals', 'k-' || $2::text, 'b.jpg', 'image/jpeg', 1, 'ready')`,
		processing, product, conversation.OutgoingImagePurpose)
	before := e.count(`SELECT count(*) FROM conversation.messages`)
	expect(t, "chữ và ảnh", e.send(http.MethodPost, path, chatSaleA,
		fmt.Sprintf(`{"text":"x","attachmentFileId":%q}`, product)), http.StatusBadRequest, "invalid_input")
	expect(t, "Page B", e.send(http.MethodPost, "/api/v1/conversations/"+e.convB.String()+"/messages", chatSaleA,
		`{"text":"x"}`), http.StatusNotFound, "not_found")
	expect(t, "đang xử lý", e.send(http.MethodPost, path, chatSaleA,
		fmt.Sprintf(`{"attachmentFileId":%q}`, processing)), http.StatusUnprocessableEntity, "attachment_not_ready")
	expect(t, "ảnh sản phẩm", e.send(http.MethodPost, path, chatSaleA,
		fmt.Sprintf(`{"attachmentFileId":%q}`, product)), http.StatusUnprocessableEntity, "attachment_rejected")
	expect(t, "ảnh không có", e.send(http.MethodPost, path, chatSaleA,
		fmt.Sprintf(`{"attachmentFileId":%q}`, uuid.New())), http.StatusNotFound, "not_found")

	lastIn := e.now.Add(-25 * time.Hour)
	e.exec(`UPDATE conversation.conversations SET last_customer_message_at = $1 WHERE id = $2`, lastIn, e.convA)
	rec := e.send(http.MethodPost, path, chatSaleA, `{"text":"x"}`)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict || body["error"] != "send_not_allowed" || body["denyReason"] != "window_closed" ||
		body["until"] != nil || body["request_id"] == nil || len(body) != 3 {
		t.Fatalf("thân send_not_allowed = %v", body)
	}

	agentIn := e.now.Add(-72 * time.Hour)
	e.exec(`UPDATE channel.channel_accounts SET human_agent_enabled = true WHERE id = $1`, e.pageA)
	e.exec(`UPDATE conversation.conversations SET last_customer_message_at = $1 WHERE id = $2`, agentIn, e.convA)
	rec = e.send(http.MethodPost, path, chatSaleA, `{"text":"x"}`)
	body = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict || body["denyReason"] != "window_closed" ||
		!sameInstant(body["until"], agentIn.Add(7*24*time.Hour).Format(time.RFC3339)) {
		t.Fatalf("thiếu humanAgent: %d %v", rec.Code, body)
	}

	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'read_only' WHERE id = $1`, e.pageA)
	expect(t, "chỉ xem", e.send(http.MethodPost, path, chatSaleA, `{"text":"x"}`), http.StatusConflict, "channel_read_only")
	if after := e.count(`SELECT count(*) FROM conversation.messages`); after != before {
		t.Fatalf("ca từ chối đã ghi %d tin", after-before)
	}
	if n := e.sendJobs(); n != 0 {
		t.Fatalf("ca từ chối đã ghi %d lệnh", n)
	}
}
