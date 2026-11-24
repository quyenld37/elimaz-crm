package web_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type shipmentBody struct {
	Status       string `json:"status"`
	CarrierKey   string `json:"carrierKey"`
	CarrierName  string `json:"carrierName"`
	TrackingCode string `json:"trackingCode"`
}

type viewWithShipment struct {
	Status        string        `json:"status"`
	DisplayStatus string        `json:"displayStatus"`
	Cancellable   bool          `json:"cancellable"`
	Shipment      *shipmentBody `json:"shipment"`
}

type changesBody struct {
	Items []struct {
		ID             uuid.UUID  `json:"id"`
		ExternalRef    string     `json:"externalRef"`
		Status         string     `json:"status"`
		DisplayStatus  string     `json:"displayStatus"`
		ShipmentStatus string     `json:"shipmentStatus"`
		DeliveredAt    *time.Time `json:"deliveredAt"`
		ChangedAt      time.Time  `json:"changedAt"`
	} `json:"items"`
	NextSince   time.Time `json:"nextSince"`
	NextAfterID string    `json:"nextAfterId"`
	HasMore     bool      `json:"hasMore"`
}

// changesAll đi hết các trang của webOrderChanges từ since (trang limit dòng); trả mã ngoài theo thứ tự và dòng cuối.
func (c *cancelEnv) changesAll(since time.Time, limit int) []string {
	c.t.Helper()
	var refs []string
	q := url.Values{"since": {since.UTC().Format(time.RFC3339Nano)}, "limit": {strconv.Itoa(limit)}}
	for range 50 {
		rec := c.get(c.readKey, ordersPath+"/changes?"+q.Encode())
		var b changesBody
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &b) != nil {
			c.t.Fatalf("changes: %d %s", rec.Code, rec.Body.String())
		}
		for _, it := range b.Items {
			refs = append(refs, it.ExternalRef)
		}
		if n := len(b.Items); n > 0 && (!b.NextSince.Equal(b.Items[n-1].ChangedAt) || b.NextAfterID != b.Items[n-1].ID.String()) {
			c.t.Fatalf("con trỏ kế khác dòng cuối: %s", rec.Body.String())
		}
		if !b.HasMore {
			return refs
		}
		q.Set("since", b.NextSince.Format(time.RFC3339Nano))
		q.Set("afterId", b.NextAfterID)
	}
	c.t.Fatal("changes không dừng")
	return nil
}

// setTimes đặt thẳng updated_at của mọi đơn và last_status_at của mọi vận đơn của tenant (chủ bảng; một đồng hồ của
// test).
func (c *cancelEnv) setTimes(orders, shipments time.Time) {
	c.t.Helper()
	c.staff.Exec(c.t, c.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(c.t.Context(), "UPDATE ord.orders SET updated_at = $1", orders); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(c.t.Context(), "UPDATE fulfillment.shipments SET last_status_at = $1", shipments)
		return err //nolint:wrapcheck // test
	})
}

// setShip đặt thẳng trạng thái, mốc đổi trạng thái (và delivered_at khi giao) của vận đơn hiện hành của đơn id.
func (c *cancelEnv) setShip(id uuid.UUID, status string, at time.Time) {
	c.t.Helper()
	c.staff.Exec(c.t, c.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(c.t.Context(), `UPDATE fulfillment.shipments SET status = $2::text, last_status_at = $3::timestamptz,
			tracking_code = coalesce(tracking_code, 'TR' || right(id::text, 12)), delivered_at = CASE WHEN $2::text = 'delivered' THEN $3::timestamptz END
			WHERE id = (SELECT current_shipment_id FROM ord.orders WHERE id = $1)`, id, status, at)
		return err //nolint:wrapcheck // test
	})
}

// [WB03]: đơn web đã Gửi hãng → shipment (hãng giả, tên tài khoản, chưa mã tra cứu), displayStatus sending (vận đơn
// creating), vẫn cancellable; vận đơn chờ thu gom có mã → awaiting_pickup, trackingCode. Không có địa chỉ giao.
func TestWebGetOrder_Shipment(t *testing.T) {
	t.Parallel()
	c := newCancelEnv(t)
	v := c.product("PS", "[]").Variants[0].ID
	c.stock(v, 5)
	ref, id := c.placeOne(v, 21)
	c.confirm(id)
	svc, account := c.shipper()
	if err := c.shipVia(svc, account, id); err != nil {
		t.Fatal(err)
	}
	read := func() viewWithShipment {
		rec := c.get(c.readKey, ordersPath+"/"+ref)
		var got viewWithShipment
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatalf("tra đơn: %d %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "Lê Lợi") || strings.Contains(rec.Body.String(), "provinceCode") {
			t.Fatalf("lộ địa chỉ giao: %s", rec.Body.String())
		}
		return got
	}
	got := read()
	if got.Status != "confirmed" || got.DisplayStatus != "sending" || !got.Cancellable || got.Shipment == nil ||
		got.Shipment.Status != "creating" || got.Shipment.CarrierKey != "fake" || got.Shipment.CarrierName != "Hãng giả" ||
		got.Shipment.TrackingCode != "" {
		t.Fatalf("đơn đã gửi hãng: %+v %+v", got, got.Shipment)
	}
	c.staff.Exec(t, c.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE fulfillment.shipments SET status = 'awaiting_pickup', tracking_code = 'FK123'
			WHERE id = (SELECT current_shipment_id FROM ord.orders WHERE id = $1)`, id)
		return err //nolint:wrapcheck // test
	})
	if got = read(); got.DisplayStatus != "awaiting_pickup" || got.Shipment.TrackingCode != "FK123" {
		t.Fatalf("chờ thu gom: %+v %+v", got, got.Shipment)
	}
}

// Luồng thay đổi: ba đơn web (W1 chưa vận đơn, W2, W3 đã Gửi hãng) có mốc t0; since = t0 + 1s → không đơn nào. Vận đơn
// W2 sang shipping lúc t0 + 2s (dòng đơn không đổi) → đúng W2, nhãn shipping. Vận đơn W3 giao lúc t0 + 3s → W2, W3
// theo thứ tự, W3 có deliveredAt; trang 1 dòng ra đủ, mỗi đơn một lần, kể cả khi ba đơn cùng changedAt (keyset theo
// id); đơn tay và đơn kênh web không mã ngoài không vào luồng; limit 501 → 400.
func TestWebOrderChanges_HTTP(t *testing.T) {
	t.Parallel()
	c := newCancelEnv(t)
	v := c.product("PCH", "[]").Variants[0].ID
	c.stock(v, 10)
	svc, account := c.shipper()
	refs := make([]string, 3)
	ids := make([]uuid.UUID, 3)
	for i := range 3 {
		refs[i], ids[i] = c.placeOne(v, 30+i)
	}
	for _, id := range ids[1:] {
		c.confirm(id)
		if err := c.shipVia(svc, account, id); err != nil {
			t.Fatal(err)
		}
	}
	// Hai đơn không thuộc luồng: đơn tay (kênh manual) và đơn kênh web không mã ngoài (nhân viên nhập).
	_, manual := c.placeOne(v, 40)
	_, noRef := c.placeOne(v, 41)
	c.staff.Exec(t, c.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), "UPDATE ord.orders SET channel = 'manual' WHERE id = $1", manual); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), "UPDATE ord.orders SET external_ref = NULL WHERE id = $1", noRef)
		return err //nolint:wrapcheck // test
	})
	t0 := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	c.setTimes(t0, t0)
	if got := c.changesAll(t0.Add(-time.Second), 100); len(got) != 3 || slices.Contains(got, "") {
		t.Fatalf("từ trước t0 (đơn tay, đơn không mã ngoài không được có): %v", got)
	}
	if got := c.changesAll(t0.Add(time.Second), 100); len(got) != 0 {
		t.Fatalf("sau t0 chưa đổi gì: %v", got)
	}
	c.setShip(ids[1], "shipping", t0.Add(2*time.Second))
	rec := c.get(c.readKey, ordersPath+"/changes?since="+url.QueryEscape(t0.Add(time.Second).Format(time.RFC3339)))
	var b changesBody
	if json.Unmarshal(rec.Body.Bytes(), &b) != nil || len(b.Items) != 1 || b.Items[0].ExternalRef != refs[1] ||
		b.Items[0].ShipmentStatus != "shipping" || b.Items[0].DisplayStatus != "shipping" ||
		!b.Items[0].ChangedAt.Equal(t0.Add(2*time.Second)) || b.Items[0].DeliveredAt != nil || b.HasMore {
		t.Fatalf("đổi vận đơn W2: %d %s", rec.Code, rec.Body.String())
	}
	c.setShip(ids[2], "delivered", t0.Add(3*time.Second))
	if got := c.changesAll(t0.Add(time.Second), 1); strings.Join(got, ",") != refs[1]+","+refs[2] {
		t.Fatalf("W2, W3 trang 1 dòng: %v", got)
	}
	rec = c.get(c.readKey, ordersPath+"/changes?since="+url.QueryEscape(t0.Add(2500*time.Millisecond).Format(time.RFC3339Nano)))
	if json.Unmarshal(rec.Body.Bytes(), &b) != nil || len(b.Items) != 1 || b.Items[0].DeliveredAt == nil ||
		!b.Items[0].DeliveredAt.Equal(t0.Add(3*time.Second)) {
		t.Fatalf("W3 đã giao: %s", rec.Body.String())
	}
	c.setTimes(t0.Add(5*time.Second), t0)
	got := c.changesAll(t0.Add(4*time.Second), 1)
	seen := map[string]int{}
	for _, r := range got {
		seen[r]++
	}
	if len(got) != 3 || seen[refs[0]] != 1 || seen[refs[1]] != 1 || seen[refs[2]] != 1 {
		t.Fatalf("cùng changedAt, trang 1 dòng: %v", got)
	}
	if rec := c.get(c.readKey, ordersPath+"/changes?since="+url.QueryEscape(t0.Format(time.RFC3339))+"&limit=501"); rec.Code != http.StatusBadRequest {
		t.Fatalf("limit 501: %d %s", rec.Code, rec.Body.String())
	}
}
