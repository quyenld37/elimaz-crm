package livetest

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// Available đọc "có thể bán" (tổng các kho) của biến thể v trong tenant (Task 173).
func Available(t testing.TB, s *testkit.Staff, tenant, v uuid.UUID) int {
	t.Helper()
	var n int
	s.Exec(t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT coalesce(sum(available), 0)::int FROM inventory.stock_available_v
			WHERE variant_id = $1`, v).Scan(&n)
	})
	return n
}

// PrivateReplies đếm lần máy Meta giả fg nhận nhắn riêng comment ext của Page page (POST {page}/messages có
// recipient.comment_id).
func PrivateReplies(fg *fakegraph.Server, page, ext string) int {
	n := 0
	for _, c := range fg.Calls(page + "/messages") {
		var b struct {
			Recipient struct {
				CommentID string `json:"comment_id"`
			} `json:"recipient"`
		}
		if json.Unmarshal(c.Body, &b) == nil && b.Recipient.CommentID == ext {
			n++
		}
	}
	return n
}
