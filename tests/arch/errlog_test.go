package arch

import (
	"go/ast"
	"path/filepath"
	"strings"
	"testing"
)

// isSlogCallName: hàm của gói slog và phương thức của *slog.Logger nhận thuộc tính log.
func isSlogCallName(name string) bool {
	switch name {
	case "Debug", "Info", "Warn", "Error", "DebugContext", "InfoContext", "WarnContext", "ErrorContext",
		"Log", "LogAttrs", "With", "String", "Any", "Group":
		return true
	}
	return false
}

// findRawErrorLogs trả mọi lời gọi slog (slog.X(...) hay <logger>.X(...)) có tham số là x.Error() hoặc một biến lỗi
// thô (tên err, ...Err): nội dung lỗi phải qua logging.Err / logging.ErrorText (bộ che dữ liệu cá nhân, Task 319).
// Bỏ qua tệp _test.go và chính gói logging.
func findRawErrorLogs(t *testing.T, root string, includeTestdata bool) []string {
	t.Helper()
	return findSelectorCalls(t, root, includeTestdata, "internal/platform/logging/",
		func(g goFile, call *ast.CallExpr, sel *ast.SelectorExpr) []string {
			if !isSlogCallName(sel.Sel.Name) || !isSlogReceiver(sel.X) {
				return nil
			}
			var out []string
			for _, a := range call.Args {
				if isRawError(a) {
					out = append(out, g.pos(a.Pos())+": nội dung lỗi thô trong "+sel.Sel.Name)
				}
			}
			return out
		})
}

// isSlogReceiver: gói slog, hoặc biểu thức có tên gợi logger (logger, log, l, Logger, ….Logger, ….logger()).
func isSlogReceiver(x ast.Expr) bool {
	switch v := x.(type) {
	case *ast.Ident:
		return v.Name == "slog" || v.Name == "l" || v.Name == "log" || strings.Contains(strings.ToLower(v.Name), "logger")
	case *ast.SelectorExpr:
		return strings.Contains(strings.ToLower(v.Sel.Name), "logger")
	case *ast.CallExpr:
		return isSlogReceiver(v.Fun)
	}
	return false
}

// isRawError: x.Error() không tham số, hoặc biến tên err / kết thúc bằng Err.
func isRawError(a ast.Expr) bool {
	switch v := a.(type) {
	case *ast.CallExpr:
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Error" && len(v.Args) == 0 {
			return true
		}
	case *ast.Ident:
		return v.Name == "err" || strings.HasSuffix(v.Name, "Err")
	}
	return false
}

func TestRawErrorLogReportsSample(t *testing.T) {
	got := findRawErrorLogs(t, filepath.Join(repoRoot(t), "tests/arch/testdata/errlog"), true)
	assertMentions(t, got, "rawerr.go:10", "rawerr.go:11", "rawerr.go:12", "rawerr.go:13")
}

func TestRawErrorLogProductCode(t *testing.T) {
	t.Parallel()
	if got := findRawErrorLogs(t, repoRoot(t), false); len(got) != 0 {
		t.Fatalf("nội dung lỗi thô vào log (dùng logging.Err hoặc logging.ErrorText):\n%s", strings.Join(got, "\n"))
	}
}
