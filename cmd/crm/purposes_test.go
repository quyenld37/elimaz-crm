package main

import (
	"testing"

	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/testkit"
)

// Bước khởi động của api, worker (Task M1-41): sau registerFilePurposes sổ mục đích tệp của tiến trình khớp đúng bảng
// platform.file_purposes sau mọi migration (kể cả report_export, customer_export của M6, Task 226, 0604); bỏ một lời
// gọi đăng ký là CheckPurposes đỏ, tiến trình không khởi động.
func TestRegisterFilePurposes_MatchTable(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	registerFilePurposes()
	if err := files.CheckPurposes(t.Context(), s.App); err != nil {
		t.Fatalf("sổ mục đích tệp lúc khởi động khác bảng: %v", err)
	}
}
