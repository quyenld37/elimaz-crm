package fulfillment

import "elimaz.com.vn/crm/internal/platform/commands"

// Cửa cho test gói fulfillment_test: hai hàm phân loại lỗi của lệnh tra (bảng ca ở track_command_test.go).

// TransientDBError là transientDBError.
func TransientDBError(err error) bool { return transientDBError(err) }

// ClassifyTrackError là classifyTrackError.
func ClassifyTrackError(err error) (commands.Outcome, string) { return classifyTrackError(err) }
