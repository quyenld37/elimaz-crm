package xlsx_test

import (
	"bytes"
	"testing"

	"elimaz.com.vn/crm/internal/platform/xlsx"
	"elimaz.com.vn/crm/internal/platform/xlsx/xlsxtest"
)

// FuzzWriterString: chuỗi bất kỳ (hạt giống ở testdata/fuzz/FuzzWriterString) ghi qua Writer (làm tiêu đề và ô dữ
// liệu) → sheet1.xml đọc được bằng encoding/xml, không có <f>, và giá trị đọc lại bằng Sanitize(s).
func FuzzWriterString(f *testing.F) {
	for _, s := range []string{"", "=1+1", "-", "\x00", "ộ", "_x005F_x0041_"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		var buf bytes.Buffer
		w, err := xlsx.NewWriter(&buf, "S", []xlsx.Column{{Title: s, Kind: xlsx.KindString}})
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Row(xlsx.Str(s)); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		checkSingleColumn(t, buf.Bytes(), xlsx.Sanitize(s))
	})
}

// checkSingleColumn: mọi dòng của tệp một cột đọc lại bằng want (ô trống đọc là "").
func checkSingleColumn(t *testing.T, data []byte, want string) {
	t.Helper()
	b, err := xlsxtest.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if fp := b.FormulaParts(); len(fp) != 0 {
		t.Fatalf("có <f> hoặc XML hỏng: %v", fp)
	}
	rows, err := b.Rows(sheetPart)
	if err != nil {
		t.Fatalf("sheet1.xml không đọc được: %v", err)
	}
	for _, r := range rows {
		got := ""
		if len(r.Cells) > 0 {
			got = r.Cells[0].Value
		}
		if got != want {
			t.Fatalf("dòng %d đọc lại %q, mong %q", r.N, got, want)
		}
	}
}
