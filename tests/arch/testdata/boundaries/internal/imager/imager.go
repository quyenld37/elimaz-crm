// Package imager: luật (g) — imager không nối database. Nhập thư viện chuẩn là hợp lệ.
package imager

import (
	_ "context"

	_ "github.com/jackc/pgx/v5"
)
