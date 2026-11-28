package main

import (
	"bytes"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/platform/config"
)

// Khung lệnh seed-staging (Task 260): production → thoát 2, không ghi gì (Q21); môi trường khác → báo "chưa làm",
// thoát 1. Task 275 thay test này bằng test đủ của lệnh.
func TestSeedStaging_Frame(t *testing.T) {
	t.Parallel()
	if !isSeedStagingCommand([]string{"seed-staging", "--seed", "42"}) || isSeedStagingCommand([]string{"seed-dev"}) {
		t.Fatal("nhận dạng lệnh seed-staging sai")
	}
	var out bytes.Buffer
	if code := seedStagingExit(runSeedStaging(t.Context(), config.Config{Env: "production"}, nil), &out); code != exitConfig ||
		!strings.Contains(out.String(), errSeedStagingProduction.Error()) {
		t.Fatalf("production: thoát %d, %q", code, out.String())
	}
	out.Reset()
	if code := seedStagingExit(runSeedStaging(t.Context(), config.Config{Env: "staging"}, nil), &out); code != exitFail ||
		!strings.Contains(out.String(), "chưa làm") {
		t.Fatalf("staging (khung): thoát %d, %q", code, out.String())
	}
}
