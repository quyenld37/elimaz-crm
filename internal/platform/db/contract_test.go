package db_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/platform/db"
)

func TestLintContract(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want int
	}{
		{"drop column", "ALTER TABLE t DROP COLUMN c;", 1},
		{"drop column có contract", "-- contract: v0.3.0\nALTER TABLE t DROP COLUMN c;", 0},
		{"drop không ghi COLUMN", "ALTER TABLE t DROP c;", 1},
		{"drop constraint là mở rộng", "ALTER TABLE t DROP CONSTRAINT t_c_check;", 0},
		{"drop not null là mở rộng", "ALTER TABLE t ALTER COLUMN c DROP NOT NULL;", 0},
		{"add column", "ALTER TABLE t ADD COLUMN c int;", 0},
		{"create index", "CREATE INDEX t_c ON t (c);", 0},
		{"drop table", "-- +goose Up\nDROP TABLE platform.x;", 1},
		{"rename", "ALTER TABLE t RENAME COLUMN a TO b;", 1},
		{"đổi kiểu", "ALTER TABLE t ALTER COLUMN c TYPE bigint;", 1},
		{"đổi kiểu SET DATA", "ALTER TABLE t ALTER COLUMN c SET DATA TYPE bigint;", 1},
		{"set not null", "ALTER TABLE t ALTER COLUMN c SET NOT NULL;", 1},
		{"contract không ngay phía trên", "-- contract: v0.3.0\n\nALTER TABLE t DROP COLUMN c;", 1},
		{"hai câu, một có contract", "-- contract: v0.3.0\nDROP TABLE a;\nDROP TABLE b;", 1},
		{"câu nhiều dòng", "-- contract: v0.3.0\nALTER TABLE t\n  DROP COLUMN c;", 0},
		{"từ khóa trong chú thích", "-- DROP TABLE x\nCREATE TABLE y (id int);", 0},
		{"chữ thường", "alter table t drop column c;", 1},
		{"hai DROP, cái đầu xóa cột", "ALTER TABLE t DROP c, DROP CONSTRAINT k;", 1},
		{"Down đứng trước Up: vẫn xét phần Up", "-- +goose Down\nDROP TABLE a;\n-- +goose Up\nALTER TABLE t DROP COLUMN c;", 1},
		{"phần Down không chạy nên không xét", "-- +goose Up\nCREATE TABLE a (id int);\n-- +goose Down\nDROP TABLE a;", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := db.LintContract(c.sql); len(got) != c.want {
				t.Fatalf("LintContract(%q) = %q, muốn %d vi phạm", c.sql, got, c.want)
			}
		})
	}
}
