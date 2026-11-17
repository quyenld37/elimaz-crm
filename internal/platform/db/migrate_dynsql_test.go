package db_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
)

// Câu dựng sẵn của migrate_dynsql.go (tạo schema platform trước goose) chạy được cả khi schema đã có.
func TestMigrateBootstrapWhenPlatformSchemaExists(t *testing.T) {
	_, owner, _ := testkit.CreateDatabase(t)
	if _, err := ownerConn(t, owner).Exec(t.Context(), "CREATE SCHEMA platform"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(t.Context(), owner); err != nil {
		t.Fatalf("Migrate khi schema platform đã có: %v", err)
	}
	if !regclassExists(t, owner, "platform.goose_db_version") {
		t.Fatal("thiếu platform.goose_db_version")
	}
}
