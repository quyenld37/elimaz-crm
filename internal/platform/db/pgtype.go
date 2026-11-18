package db

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// NullUUID đổi con trỏ uuid sang tham số cột uuid có thể rỗng của sqlc: nil → NULL, khác nil → giá trị (kể cả
// uuid.Nil). Cột có dùng uuid.Nil để chỉ "không có" thì không dùng hàm này: tự dựng pgtype.UUID với Valid riêng.
func NullUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}
