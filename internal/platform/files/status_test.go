package files_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
)

// Status trả kích thước ảnh đã xử lý và lý do từ chối (màn banner, Task 488): ready → rộng, cao; rejected → lý do;
// đang chờ xử lý → không có cả hai.
func TestStatus_SizeAndRejectReason(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.must(e.a, func(tx pgx.Tx) error {
		cases := []struct {
			name string
			get  func() files.FileStatus
			want files.FileStatus
		}{
			{
				"ready", func() files.FileStatus { return mustStatus(t, tx, filestest.Image(t, tx, e.a, "product_image")) },
				files.FileStatus{Purpose: "product_image", Status: files.StatusReady, Width: 2000, Height: 3000},
			},
			{
				"rejected", func() files.FileStatus { return mustStatus(t, tx, filestest.Rejected(t, tx, e.a, "product_image")) },
				files.FileStatus{Purpose: "product_image", Status: files.StatusRejected, RejectReason: "not_image"},
			},
			{
				"uploaded", func() files.FileStatus { return mustStatus(t, tx, filestest.Uploaded(t, tx, e.a, "product_image")) },
				files.FileStatus{Purpose: "product_image", Status: "uploaded"},
			},
		}
		for _, c := range cases {
			if got := c.get(); got != c.want {
				t.Errorf("%s: %+v, mong %+v", c.name, got, c.want)
			}
		}
		return nil
	})
}

func mustStatus(t *testing.T, tx pgx.Tx, id uuid.UUID) files.FileStatus {
	t.Helper()
	st, err := files.Status(t.Context(), tx, id)
	if err != nil {
		t.Fatal(err)
	}
	return st
}
