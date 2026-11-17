// Package migrations nhúng các tệp migration SQL của goose vào tệp chạy (ADR-44: "build một lần").
//
// Dải số của M0 là 0001–0099 (Global Constraints, Q-X7). Không sửa tệp đã commit; thêm tệp mới.
package migrations

import "embed"

// FS chứa mọi tệp *.sql ở gốc; db.Migrate đọc từ đây.
//
//go:embed *.sql
var FS embed.FS
