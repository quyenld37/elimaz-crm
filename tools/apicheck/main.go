// Lệnh apicheck chặn thay đổi phá vỡ của hợp đồng API chưa được chủ shop duyệt (kế hoạch M0 bản 5, Task 6;
// luật .claude/rules/openapi.md mục 5). `make oasdiff` chạy:
//
//	go run ./tools/apicheck -base-ref main -rev openapi/openapi.yaml -approved openapi/breaking-approved.yaml
//
// Nó gọi `oasdiff breaking` (v1.38.0, tools/versions.mk) giữa bản gốc (tệp `-base`, hoặc tệp cùng đường dẫn ở nhánh
// `-base-ref`) và `-rev`. Mỗi thay đổi phá vỡ (mức ERR hoặc WARN) phải có dòng trong `-approved` cùng operationId,
// có lý do và ngày chủ shop OK; thiếu thì thoát 1 và nêu operationId. Nhánh gốc chưa có tệp (lần đầu) thì bỏ qua.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout))
}

// change là một dòng của `oasdiff breaking -f json`.
type change struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	Level       int    `json:"level"`
	Operation   string `json:"operation"`
	OperationID string `json:"operationId"`
	Path        string `json:"path"`
}

func run(ctx context.Context, args []string, out io.Writer) int {
	fs := flag.NewFlagSet("apicheck", flag.ContinueOnError)
	fs.SetOutput(out)
	base := fs.String("base", "", "tệp hợp đồng gốc")
	baseRef := fs.String("base-ref", "", "nhánh git chứa bản gốc (cùng đường dẫn với -rev); dùng khi không có -base")
	rev := fs.String("rev", "", "tệp hợp đồng mới")
	approvedPath := fs.String("approved", "", "tệp breaking-approved.yaml")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *rev == "" || (*base == "") == (*baseRef == "") {
		_, _ = fmt.Fprintln(out, "apicheck: cần -rev và đúng một trong -base, -base-ref")
		return 2
	}
	basePath := *base
	if basePath == "" {
		p, ok, err := fromRef(ctx, *baseRef, *rev)
		if err != nil {
			_, _ = fmt.Fprintln(out, "apicheck:", err)
			return 1
		}
		if !ok {
			_, _ = fmt.Fprintf(out, "apicheck: %s chưa có %s, bỏ qua (lần đầu)\n", *baseRef, *rev)
			return 0
		}
		defer func() { _ = os.Remove(p) }()
		basePath = p
	}
	changes, err := breaking(ctx, basePath, *rev)
	if err != nil {
		_, _ = fmt.Fprintln(out, "apicheck:", err)
		return 1
	}
	approved := map[string]bool{}
	if *approvedPath != "" {
		b, err := os.ReadFile(*approvedPath)
		if err != nil {
			_, _ = fmt.Fprintln(out, "apicheck:", err)
			return 1
		}
		var problems []string
		approved, problems = parseApproved(b)
		for _, p := range problems {
			_, _ = fmt.Fprintf(out, "apicheck: %s: dòng không hợp lệ, không tính: %s\n", *approvedPath, p)
		}
	}
	failed := false
	for _, c := range changes {
		status := "CHƯA DUYỆT"
		if approved[c.OperationID] {
			status = "đã duyệt"
		} else {
			failed = true
		}
		_, _ = fmt.Fprintf(out, "%s: %s %s %s (%s): %s\n", status, c.OperationID, c.Operation, c.Path, c.ID, c.Text)
	}
	if failed {
		_, _ = fmt.Fprintln(out, "apicheck: có thay đổi phá vỡ chưa có dòng trong breaking-approved.yaml "+
			"(operationId, reason, approvedOn); đi theo mở rộng rồi thu hẹp")
		return 1
	}
	return 0
}

// fromRef ghi tệp path (tương đối so với thư mục hiện tại) ở nhánh ref ra một tệp tạm; ok=false khi nhánh có mà
// tệp chưa có. Nhánh không có là lỗi: không được lặng lẽ bỏ qua kiểm (ví dụ CI chưa tải nhánh main).
func fromRef(ctx context.Context, ref, path string) (string, bool, error) {
	if out, err := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet", ref+"^{commit}").CombinedOutput(); err != nil { //nolint:gosec // công cụ dev; tham số từ Makefile
		return "", false, fmt.Errorf("không có nhánh %s để so: %w %s", ref, err, bytes.TrimSpace(out))
	}
	rel := filepath.ToSlash(filepath.Clean(path))
	cmd := exec.CommandContext(ctx, "git", "show", ref+":./"+rel) //nolint:gosec // công cụ dev; tham số từ Makefile
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	content, err := cmd.Output()
	if err != nil {
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			return "", false, nil // nhánh có, tệp chưa có
		}
		return "", false, fmt.Errorf("git show %s:%s: %w", ref, rel, err)
	}
	f, err := os.CreateTemp("", "apicheck-base-*.yaml")
	if err != nil {
		return "", false, fmt.Errorf("tạo tệp tạm: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(content); err != nil {
		return "", false, fmt.Errorf("ghi tệp tạm: %w", err)
	}
	return f.Name(), true, nil
}

// breaking chạy oasdiff và trả mọi thay đổi phá vỡ (mức ERR và WARN).
func breaking(ctx context.Context, base, rev string) ([]change, error) {
	cmd := exec.CommandContext(ctx, "oasdiff", "breaking", base, rev, "-f", "json") //nolint:gosec // công cụ dev; tham số từ Makefile
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("oasdiff breaking: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var changes []change
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(out, &changes); err != nil {
		return nil, fmt.Errorf("đọc kết quả oasdiff: %w", err)
	}
	return changes, nil
}

// Một dòng duyệt: `  - {operationId: <id>, reason: "<lý do>", approvedOn: YYYY-MM-DD}`.
var (
	approvedLine = regexp.MustCompile(`^\s*-\s*\{(.*)\}\s*$`)
	fieldRe      = regexp.MustCompile(`(\w+):\s*("[^"]*"|[^,]*)`)
	dateRe       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// parseApproved đọc các dòng duyệt; dòng thiếu operationId, reason hay approvedOn đúng dạng ngày thì không tính và
// được trả trong problems.
func parseApproved(b []byte) (map[string]bool, []string) {
	ok := map[string]bool{}
	var problems []string
	for line := range strings.SplitSeq(string(b), "\n") {
		m := approvedLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		fields := map[string]string{}
		for _, f := range fieldRe.FindAllStringSubmatch(m[1], -1) {
			fields[f[1]] = strings.Trim(strings.TrimSpace(f[2]), `"`)
		}
		id, reason, date := fields["operationId"], strings.TrimSpace(fields["reason"]), fields["approvedOn"]
		if id == "" || reason == "" || !dateRe.MatchString(date) {
			problems = append(problems, strings.TrimSpace(line)+" (cần operationId, reason, approvedOn: YYYY-MM-DD)")
			continue
		}
		ok[id] = true
	}
	return ok, problems
}
