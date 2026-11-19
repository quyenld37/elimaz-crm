package db_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/db"
)

func TestNullUUID(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	nilID := uuid.Nil
	cases := []struct {
		name string
		in   *uuid.UUID
		want pgtype.UUID
	}{
		{"nil là NULL", nil, pgtype.UUID{}},
		{"có giá trị", &id, pgtype.UUID{Bytes: id, Valid: true}},
		{"uuid.Nil không phải con trỏ nil vẫn là giá trị", &nilID, pgtype.UUID{Bytes: uuid.Nil, Valid: true}},
	}
	for _, c := range cases {
		if got := db.NullUUID(c.in); got != c.want {
			t.Errorf("%s: NullUUID = %+v, mong %+v", c.name, got, c.want)
		}
	}
}
