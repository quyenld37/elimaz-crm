package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit/livetest"
)

// [Task 176, 01 mục 6] Lên đơn trong khung chat: createOrder có conversationId → đơn lưu hội thoại, Page của hội thoại,
// kênh facebook (kể cả khi form gửi manual), người bán của Page; người không xem được hội thoại (Page khác) → 404
// not_found, không tạo đơn; Page đổi sang người bán khác → hàng của người bán cũ bị từ chối như M2 (422 mixed_sellers),
// không tạo đơn.
func TestCreateOrder_FromConversation(t *testing.T) {
	t.Parallel()
	e := newLiveClaimAPIEnv(t)
	v := e.variant(5)
	cust := livetest.Customer(t, e.staff, e.tenant, "Lan")
	cm := livetest.LiveComment(t, e.staff, e.tenant, e.page, cust, "lv-http", "u-176", "cm-176", "chốt", time.Now())
	body := fmt.Sprintf(`{"customerId":%q,"conversationId":%q,"intent":"new","shippingFee":0,"channel":"manual",
		"items":[{"variantId":%q,"qty":1}]}`, cust, cm.Conversation, v)

	w := e.send(http.MethodPost, "/api/v1/orders", liveOp, body)
	b := decodeJSON(t, w)
	if w.Code != http.StatusCreated {
		t.Fatalf("tạo đơn từ hội thoại: %d %s", w.Code, w.Body)
	}
	id, _ := b["id"].(string)
	var conv, page, seller uuid.UUID
	var channel string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT conversation_id, channel_account_id, channel, seller_id FROM ord.orders
			WHERE id = $1`, id).Scan(&conv, &page, &channel, &seller)
	})
	if conv != cm.Conversation || page != e.page || channel != "facebook" || seller != e.shop.Seller {
		t.Fatalf("đơn lưu hội thoại: conv=%s page=%s channel=%s seller=%s", conv, page, channel, seller)
	}

	count := func() (n int) {
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `SELECT count(*) FROM ord.orders`).Scan(&n)
		})
		return n
	}
	w = e.send(http.MethodPost, "/api/v1/orders", liveOther, body)
	if b := decodeJSON(t, w); w.Code != http.StatusNotFound || b["error"] != "not_found" || count() != 1 {
		t.Fatalf("Page khác: %d %s, %d đơn", w.Code, w.Body, count())
	}

	e.exec(`UPDATE channel.channel_accounts SET seller_id = $2 WHERE id = $1`, e.page, e.shop.Partner)
	w = e.send(http.MethodPost, "/api/v1/orders", liveOp, body)
	if b := decodeJSON(t, w); w.Code != http.StatusUnprocessableEntity || b["error"] != "mixed_sellers" || count() != 1 {
		t.Fatalf("Page của người bán khác: %d %s, %d đơn", w.Code, w.Body, count())
	}
}
