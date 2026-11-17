// Tệp mẫu cho sqlmode_test: bật simple protocol (phải bị báo).
package sample

import "github.com/jackc/pgx/v5"

func mode(c *pgx.ConnConfig) {
	c.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
}
