package files_test

// Ca thêm ở Task 403 (mutation testing vùng cao M1, docs/mutation.md): ListLost phân trang theo lost_at giảm dần.

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/files"
)

// Hai tệp mất: trang đầu (limit 1) là tệp mất gần nhất; trang sau con trỏ là tệp còn lại; limit 0 → không dòng nào.
func TestListLost_Pages(t *testing.T) {
	p := newProcEnv(t)
	older := p.upload(p.a, "product_image", []byte("anh-mat-truoc"))
	newer := p.upload(p.a, "product_image", []byte("anh-mat-sau"))
	for id, ago := range map[uuid.UUID]string{older: "2 hours", newer: "1 hour"} {
		p.asOwner(func(tx pgx.Tx) (bool, error) {
			tag, err := tx.Exec(t.Context(), `UPDATE platform.files SET status = 'lost', lost_at = now() - $2::interval
				WHERE id = $1`, id, ago)
			return tag.RowsAffected() == 1, err //nolint:wrapcheck // test
		})
	}
	list := func(after *files.LostCursor, limit int) []files.LostFile {
		var out []files.LostFile
		p.must(p.a, func(tx pgx.Tx) error {
			var err error
			out, err = files.ListLost(t.Context(), tx, after, limit)
			return err //nolint:wrapcheck // test
		})
		return out
	}
	first := list(nil, 1)
	if len(first) != 1 || first[0].ID != newer || first[0].Purpose != "product_image" {
		t.Fatalf("trang đầu: %+v", first)
	}
	rest := list(&files.LostCursor{At: first[0].LostAt, ID: first[0].ID}, 10)
	// Hai mốc đều do database ghi (cách nhau ~1 giờ): so với nhau, không so với đồng hồ máy chạy test.
	if len(rest) != 1 || rest[0].ID != older || !rest[0].LostAt.Before(first[0].LostAt.Add(-50*time.Minute)) {
		t.Fatalf("trang sau: %+v (trang đầu %+v)", rest, first)
	}
	if got := list(nil, 0); len(got) != 0 {
		t.Fatalf("limit 0: %+v", got)
	}
}
