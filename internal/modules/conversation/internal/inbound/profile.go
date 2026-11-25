package inbound

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OnNewCustomer xếp lệnh đọc hồ sơ kênh (tên, ảnh đại diện) cho khách customerID vừa tạo từ tin vào, kèm hội thoại
// conversationID để lệnh đọc PSID (Task 155 hiện thực; khung của Task 153).
func OnNewCustomer(context.Context, pgx.Tx, uuid.UUID, uuid.UUID) error { return nil }
