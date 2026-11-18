// Lệnh adminunits đọc tệp danh mục đơn vị hành chính hai cấp (tỉnh, xã/phường) và sinh migration dữ liệu cho bảng
// customer.admin_units (kế hoạch M2, Task M2-04).
//
//	go run ./tools/adminunits -in data/admin-units/<tệp>.csv -out migrations/0203_admin_units_data.sql \
//	    -source "<nguồn>" [-effective-from 2025-07-01]
//
// Tệp vào: CSV UTF-8, dòng tiêu đề đúng `code,level,parent_code,name`, mỗi đơn vị một dòng; level là `province` (để
// trống parent_code) hoặc `ward` (parent_code là mã một tỉnh trong tệp). Kiểm: không trùng mã, mỗi xã có tỉnh cha, tên
// không rỗng, có ít nhất một tỉnh. Lỗi thì thoát khác 0, nêu mã hoặc số dòng sai, không ghi tệp. Đúng thì in số tỉnh
// và số xã, ghi SQL: tỉnh trước xã, một câu INSERT … VALUES mỗi 500 dòng, name_search = textnorm.Fold(name).
//
// Tệp gốc đổi thì sinh migration mới với số mới (không sửa migration đã commit).
package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/platform/textnorm"
)

const (
	levelProvince = "province"
	levelWard     = "ward"
	batchSize     = 500
)

var (
	errInvalidFile = errors.New("tệp danh mục sai")
	errUsage       = errors.New("thiếu cờ")
)

type unit struct {
	code, level, parent, name string
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("adminunits", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := fs.String("in", "", "tệp CSV danh mục")
	out := fs.String("out", "", "tệp migration SQL sẽ ghi")
	source := fs.String("source", "", "nguồn dữ liệu, ghi vào cột source")
	effective := fs.String("effective-from", "2025-07-01", "ngày hiệu lực YYYY-MM-DD")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := generate(*in, *out, strings.TrimSpace(*source), *effective, stdout); err != nil {
		_, _ = fmt.Fprintf(stderr, "adminunits: %v\n", err)
		return 1
	}
	return 0
}

func generate(in, out, source, effective string, stdout io.Writer) error {
	if in == "" || out == "" || source == "" {
		return fmt.Errorf("%w: cần -in, -out, -source", errUsage)
	}
	if _, err := time.Parse(time.DateOnly, effective); err != nil {
		return fmt.Errorf("%w: -effective-from phải dạng YYYY-MM-DD", errUsage)
	}
	f, err := os.Open(in) //nolint:gosec // đường tệp do người chạy tool đưa vào
	if err != nil {
		return fmt.Errorf("mở tệp danh mục: %w", err)
	}
	defer func() { _ = f.Close() }()
	units, err := readUnits(f)
	if err != nil {
		return err
	}
	provinces, wards, err := check(units)
	if err != nil {
		return err
	}
	sql := render(slices.Concat(provinces, wards), source, effective)
	if err := os.WriteFile(out, []byte(sql), 0o600); err != nil {
		return fmt.Errorf("ghi migration: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "%d tỉnh, %d xã\n", len(provinces), len(wards))
	return nil
}

// readUnits đọc CSV: đúng tiêu đề, mỗi dòng đủ 4 cột, mã và cấp không rỗng (bỏ khoảng trắng hai đầu mọi ô).
func readUnits(r io.Reader) ([]unit, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%w: đọc CSV: %w", errInvalidFile, err)
	}
	if len(rows) == 0 || !slices.Equal(trimAll(rows[0]), []string{"code", "level", "parent_code", "name"}) {
		return nil, fmt.Errorf("%w: dòng tiêu đề phải là code,level,parent_code,name", errInvalidFile)
	}
	units := make([]unit, 0, len(rows)-1)
	for i, row := range rows[1:] {
		line := i + 2
		if len(row) != 4 {
			return nil, fmt.Errorf("%w: dòng %d có %d cột, cần 4", errInvalidFile, line, len(row))
		}
		row = trimAll(row)
		if row[0] == "" {
			return nil, fmt.Errorf("%w: dòng %d thiếu mã", errInvalidFile, line)
		}
		units = append(units, unit{code: row[0], level: row[1], parent: row[2], name: row[3]})
	}
	return units, nil
}

func trimAll(row []string) []string {
	out := make([]string, len(row))
	for i, c := range row {
		out[i] = strings.TrimSpace(strings.TrimPrefix(c, "\ufeff"))
	}
	return out
}

// check kiểm danh mục và tách tỉnh, xã (giữ thứ tự trong tệp).
func check(units []unit) (provinces, wards []unit, err error) {
	levels := make(map[string]string, len(units))
	for _, u := range units {
		if _, dup := levels[u.code]; dup {
			return nil, nil, fmt.Errorf("%w: trùng mã %s", errInvalidFile, u.code)
		}
		levels[u.code] = u.level
	}
	for _, u := range units {
		if u.name == "" {
			return nil, nil, fmt.Errorf("%w: %s không có tên", errInvalidFile, u.code)
		}
		switch u.level {
		case levelProvince:
			if u.parent != "" {
				return nil, nil, fmt.Errorf("%w: tỉnh %s không được có parent_code", errInvalidFile, u.code)
			}
			provinces = append(provinces, u)
		case levelWard:
			if levels[u.parent] != levelProvince {
				return nil, nil, fmt.Errorf("%w: xã %s trỏ tới %q không phải tỉnh trong tệp", errInvalidFile, u.code, u.parent)
			}
			wards = append(wards, u)
		default:
			return nil, nil, fmt.Errorf("%w: %s có cấp %q (chỉ province, ward)", errInvalidFile, u.code, u.level)
		}
	}
	if len(provinces) == 0 {
		return nil, nil, fmt.Errorf("%w: không có tỉnh nào", errInvalidFile)
	}
	return provinces, wards, nil
}

func render(units []unit, source, effective string) string {
	var b strings.Builder
	b.WriteString("-- Dữ liệu danh mục đơn vị hành chính hai cấp (kế hoạch M2, Task M2-04). SINH BẰNG tools/adminunits,\n")
	b.WriteString("-- không sửa tay; tệp gốc đổi thì sinh migration mới.\n\n-- +goose Up\n")
	for start := 0; start < len(units); start += batchSize {
		batch := units[start:min(start+batchSize, len(units))]
		b.WriteString("\nINSERT INTO customer.admin_units (code, level, parent_code, name, name_search, effective_from, source) VALUES")
		for i, u := range batch {
			parent := "NULL"
			if u.parent != "" {
				parent = quote(u.parent)
			}
			fmt.Fprintf(&b, "\n    (%s, %s, %s, %s, %s, DATE %s, %s)", quote(u.code), quote(u.level), parent,
				quote(u.name), quote(textnorm.Fold(u.name)), quote(effective), quote(source))
			if i < len(batch)-1 {
				b.WriteByte(',')
			}
		}
		b.WriteString(";\n")
	}
	return b.String()
}

// quote trả chuỗi SQL đặt trong nháy đơn, nháy đơn bên trong được nhân đôi (standard_conforming_strings bật mặc định).
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
