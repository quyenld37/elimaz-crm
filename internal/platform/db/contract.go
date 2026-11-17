package db

import (
	"regexp"
	"strings"
)

// Lệnh thu hẹp (ADR-26, Global Constraints): DROP TABLE, DROP COLUMN (kể cả "ALTER TABLE t DROP c"), RENAME,
// ALTER COLUMN … TYPE, SET NOT NULL. Không tính DROP CONSTRAINT, DROP DEFAULT, DROP NOT NULL (là nới ra).
var (
	contractMarkerRe = regexp.MustCompile(`^\s*--\s*contract:\s*\S`)
	dropTableRe      = regexp.MustCompile(`(?i)\bDROP\s+TABLE\b`)
	dropColumnRe     = regexp.MustCompile(`(?i)\bDROP\s+COLUMN\b`)
	alterTableRe     = regexp.MustCompile(`(?i)\bALTER\s+TABLE\b`)
	dropWordRe       = regexp.MustCompile(`(?i)\bDROP\s+(?:IF\s+EXISTS\s+)?(\w+)`)
	gooseDownRe      = regexp.MustCompile(`(?i)^\s*--\s*\+goose\s+Down\b`)
	gooseUpRe        = regexp.MustCompile(`(?i)^\s*--\s*\+goose\s+Up\b`)
	renameRe         = regexp.MustCompile(`(?i)\bRENAME\b`)
	alterTypeRe      = regexp.MustCompile(`(?i)\bALTER\s+(?:COLUMN\s+)?\S+\s+(?:SET\s+DATA\s+)?TYPE\b`)
	setNotNullRe     = regexp.MustCompile(`(?i)\bSET\s+NOT\s+NULL\b`)
)

// dropKeepsData: từ đứng sau "ALTER TABLE … DROP" mà không xóa cột.
func dropKeepsData(word string) bool {
	switch strings.ToUpper(word) {
	case "CONSTRAINT", "DEFAULT", "NOT", "EXPRESSION", "IDENTITY", "COLUMN":
		// COLUMN đã có luật riêng ở trên.
		return true
	}
	return false
}

// LintContract trả mọi câu thu hẹp trong sql không có dòng "-- contract: <bản đã ngừng dùng>" ngay phía trên dòng đầu
// của câu, chỉ trong phần Up (bỏ phần "-- +goose Down"). Câu được tách theo dấu ";" ngoài chú thích; thân hàm $$…$$ có ";" có thể bị tách sai, nên migration
// không đặt lệnh thu hẹp trong thân hàm. Migrate từ chối tệp còn vi phạm.
func LintContract(sql string) []string {
	lines := strings.Split(sql, "\n")
	var out []string
	start := -1
	var stmt strings.Builder
	flush := func() {
		if start >= 0 && isNarrowing(stmt.String()) && (start == 0 || !contractMarkerRe.MatchString(lines[start-1])) {
			out = append(out, strings.TrimSpace(lines[start]))
		}
		start = -1
		stmt.Reset()
	}
	inDown := false
	for i, line := range lines {
		// Phần Down không chạy khi migrate (Migrate chỉ đi lên): bỏ từ "-- +goose Down" tới "-- +goose Up" kế tiếp.
		switch {
		case gooseDownRe.MatchString(line):
			flush()
			inDown = true
			continue
		case gooseUpRe.MatchString(line):
			flush()
			inDown = false
			continue
		case inDown:
			continue
		}
		code, _, _ := strings.Cut(line, "--")
		if start < 0 {
			if strings.TrimSpace(code) == "" {
				continue
			}
			start = i
		}
		for {
			before, after, found := strings.Cut(code, ";")
			stmt.WriteString(before)
			stmt.WriteString(" ")
			if !found {
				break
			}
			flush()
			if strings.TrimSpace(after) == "" {
				break
			}
			start, code = i, after
		}
	}
	flush()
	return out
}

func isNarrowing(stmt string) bool {
	if alterTableRe.MatchString(stmt) {
		for _, m := range dropWordRe.FindAllStringSubmatch(stmt, -1) {
			if !dropKeepsData(m[1]) {
				return true
			}
		}
	}
	for _, re := range []*regexp.Regexp{dropTableRe, dropColumnRe, renameRe, alterTypeRe, setNotNullRe} {
		if re.MatchString(stmt) {
			return true
		}
	}
	return false
}
