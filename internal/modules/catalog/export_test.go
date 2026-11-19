package catalog

import "time"

// NewImportApplierWithClock dựng việc áp đọc giờ bằng now (test dời đồng hồ để hết ngân sách giữa lúc ghi).
func NewImportApplierWithClock(w Writer, now func() time.Time) *ImportApplier {
	return &ImportApplier{w: w, now: now}
}
