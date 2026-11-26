package testkit

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Conversation chèn (bằng chủ bảng) một hội thoại inbox của khách customerID trên Page page với mã luồng thread, tin
// khách cuối lúc now(); trả mã hội thoại (Task 160).
func (s *Staff) Conversation(t testing.TB, tenant, page, customerID uuid.UUID, thread string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	s.Exec(t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO conversation.conversations
			(tenant_id, channel_account_id, customer_id, kind, external_thread_ref, last_message_at, last_customer_message_at)
			VALUES ($1, $2, $3, 'inbox', $4, now(), now()) RETURNING id`, tenant, page, customerID, thread).Scan(&id)
	})
	return id
}

// TrafficSource chèn một nguồn đơn (đang bật hay không) và trả mã (Task 160).
func (s *Staff) TrafficSource(t testing.TB, tenant uuid.UUID, name string, active bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	s.Exec(t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO ord.traffic_sources (tenant_id, name, active, position)
			VALUES ($1, $2, $3, 0) RETURNING id`, tenant, name, active).Scan(&id)
	})
	return id
}
