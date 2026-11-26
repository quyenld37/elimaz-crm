package loadstats_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/loadstats"
)

// evID dựng mã sự kiện như fb_common.js (m_/lc_<run>_<ms>_<VU>_<lượt>).
func evID(kind, run string, at time.Time, vu, iter int) string {
	return fmt.Sprintf("%s_%s_%d_%d_%d", kind, run, at.UnixMilli(), vu, iter)
}

// Câu đọc số đo trên PostgreSQL thật: chỉ sự kiện khách của lượt (theo tiền tố mã, chiều vào), đếm dòng và mã khác
// nhau, comment thuộc phiên live, tin gửi ra theo trạng thái, số dư tồn và dòng giữ vượt, việc dead theo loại,
// histogram cộng theo tên; RLS: tenant khác không lẫn vào.
func TestReadFBStats(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	ctx := t.Context()
	tenant, other := s.Tenant(t, "Thử tải"), s.Tenant(t, "Khác")
	page := s.ChannelAccount(t, tenant, "facebook", "100000000000001", "Shop")
	conv := s.Conversation(t, tenant, page, customerIn(t, s, tenant), "t1")
	otherPage := s.ChannelAccount(t, other, "facebook", "100000000000009", "Shop khác")
	otherConv := s.Conversation(t, other, otherPage, customerIn(t, s, other), "t9")
	var from time.Time
	if err := s.Owner.Pool.QueryRow(ctx, "SELECT now() - interval '1 minute'").Scan(&from); err != nil {
		t.Fatal(err)
	}
	seedFBStats(t, s, tenant, page, conv, other, otherPage, otherConv)
	hist := func(i int) []int {
		h := make([]int, 12)
		h[i] = 5
		return h
	}
	for i, name := range []string{"webhook_to_signal_ms", "webhook_to_signal_ms", "job_ms"} {
		if _, err := s.Owner.Pool.Exec(ctx, `INSERT INTO platform.metrics_minute (bucket, process, name, label, count, sum, max, hist)
			VALUES (date_trunc('minute', now()), $1, $2, 'facebook', 5, 5, 5, $3)`, fmt.Sprintf("p%d", i), name, hist(i)); err != nil {
			t.Fatal(err)
		}
	}

	events, err := loadstats.Events(ctx, s.Owner, tenant, "m_fb1_", "lc_fb1_")
	if err != nil || len(events) != 4 {
		t.Fatalf("sự kiện của lượt fb1 ở tenant thử: %+v %v", events, err)
	}
	for _, e := range events {
		if e.Conversation != conv.String() {
			t.Errorf("hội thoại: %+v", e)
		}
	}
	st, err := loadstats.Read(ctx, s.Owner, tenant, "m_fb1_", "lc_fb1_", from)
	if err != nil {
		t.Fatal(err)
	}
	want := loadstats.Stats{
		InboxRows: 2, InboxDistinct: 2, CommentRows: 2, CommentDistinct: 2, CommentLive: 1,
		Sends: 3, SendsSent: 1, SendsPending: 1, SendsBad: 1, OnHand: 800, Held: 0, Dead: 1, Backlog: 1,
	}
	got := st
	got.At, got.DeadKinds, got.Hist, got.Deadlocks = time.Time{}, nil, nil, 0
	if !reflect.DeepEqual(got, want) {
		t.Errorf("số đo:\n có  %+v\n muốn %+v", got, want)
	}
	if st.DeadKinds["inbound.facebook"] != 1 {
		t.Errorf("loại việc dead: %v", st.DeadKinds)
	}
	if h := st.Hist["webhook_to_signal_ms"]; len(h) < 2 || h[0] != 5 || h[1] != 5 {
		t.Errorf("histogram cộng hai tiến trình: %v", st.Hist)
	}
	if _, ok := st.Hist["job_ms"]; ok {
		t.Error("chỉ đọc các chỉ số HistNames")
	}
}

func customerIn(t *testing.T, s *testkit.Staff, tenant uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	s.Exec(t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO customer.customers (tenant_id, full_name) VALUES ($1, 'Khách thử')
			RETURNING id`, tenant).Scan(&id)
	})
	return id
}

// seedFBStats chèn dữ liệu của TestReadFBStats: tin, comment của lượt fb1 (và lượt khác, tenant khác), tin gửi ra theo
// trạng thái, hai biến thể có tồn, việc dead và pending.
func seedFBStats(t *testing.T, s *testkit.Staff, tenant, page, conv, other, otherPage, otherConv uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	now := time.Now()
	msg := func(tn, acc, c uuid.UUID, ext, dir, kind, origin, status string, live bool) {
		var st any
		if status != "" {
			st = status
		}
		if err := s.TryExec(ctx, tn, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id,
				external_id, direction, kind, origin, send_status, is_live, sent_at)
				VALUES ($1, $2, $3, nullif($4, ''), $5, $6, $7, $8, $9, now())`, tn, c, acc, ext, dir, kind, origin, st, live)
			if err != nil {
				return fmt.Errorf("chèn tin thử: %w", err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	msg(tenant, page, conv, evID("m", "fb1", now, 1, 1), "in", "message", "customer", "", false)
	msg(tenant, page, conv, evID("m", "fb1", now, 1, 2), "in", "message", "customer", "", false)
	msg(tenant, page, conv, evID("lc", "fb1", now, 2, 1), "in", "comment", "customer", "", true)
	msg(tenant, page, conv, evID("lc", "fb1", now, 2, 2), "in", "comment", "customer", "", false)
	msg(tenant, page, conv, evID("m", "fb0", now, 1, 1), "in", "message", "customer", "", false) // lượt khác
	msg(tenant, page, conv, "", "out", "message", "staff", "sent", false)
	msg(tenant, page, conv, "", "out", "message", "staff", "in_doubt", false)
	msg(tenant, page, conv, "", "out", "private_reply", "automation", "queued", false)
	msg(other, otherPage, otherConv, evID("m", "fb1", now, 9, 9), "in", "message", "customer", "", false)
	if err := s.TryExec(ctx, tenant, func(tx pgx.Tx) error {
		shop := invtest.NewShop(t, tx, tenant)
		shop.Variant(t, tx, shop.Seller, 100_000, 500)
		shop.Variant(t, tx, shop.Seller, 100_000, 300)
		_, err := tx.Exec(ctx, `INSERT INTO platform.jobs (tenant_id, kind, mode, dedupe_key, on_lease_expired, max_attempts, status)
			VALUES ($1, 'inbound.facebook', 'transactional', 'lt-dead-1', 'retry', 8, 'dead'),
			       ($1, 'inbound.facebook', 'transactional', 'lt-pending-1', 'retry', 8, 'pending')`, tenant)
		if err != nil {
			return fmt.Errorf("chèn việc thử: %w", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
