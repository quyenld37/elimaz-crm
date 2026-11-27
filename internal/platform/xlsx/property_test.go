package xlsx_test

import (
	"bytes"
	"strconv"
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/xlsx"
	"elimaz.com.vn/crm/internal/platform/xlsx/xlsxtest"
)

// TestWriterRoundTripProperty: bảng ngẫu nhiên (chữ, số, tiền, ô trống) ghi rồi đọc lại bằng xlsxtest: chữ bằng
// Sanitize(s), số bằng đúng giá trị int64, ô trống không có trong tệp, không phần nào có <f>; Rows() bằng số dòng.
func TestWriterRoundTripProperty(t *testing.T) {
	t.Parallel()
	kinds := []xlsx.Kind{xlsx.KindString, xlsx.KindInt, xlsx.KindMoney}
	rapid.Check(t, func(t *rapid.T) {
		ncol := rapid.IntRange(1, 6).Draw(t, "ncol")
		cols := make([]xlsx.Column, ncol)
		for i := range cols {
			cols[i] = xlsx.Column{Title: rapid.String().Draw(t, "title"), Kind: rapid.SampledFrom(kinds).Draw(t, "kind")}
		}
		nrow := rapid.IntRange(0, 20).Draw(t, "nrow")
		want := map[string]string{} // ô → giá trị mong; ô không có khóa là ô trống
		var buf bytes.Buffer
		w, err := xlsx.NewWriter(&buf, "S", cols)
		if err != nil {
			t.Fatal(err)
		}
		for r := range nrow {
			cells := make([]xlsx.Cell, ncol)
			for c := range cells {
				var v string
				cells[c], v = randomCell(t)
				if v != "" {
					want[string(rune('A'+c))+strconv.Itoa(r+2)] = v
				}
			}
			if err := w.Row(cells...); err != nil {
				t.Fatal(err)
			}
		}
		if w.Rows() != nrow {
			t.Fatalf("Rows() = %d, mong %d", w.Rows(), nrow)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		checkCells(t, buf.Bytes(), want)
	})
}

// randomCell trả một ô ngẫu nhiên và giá trị mong đọc lại ("" là ô không có trong tệp).
func randomCell(t *rapid.T) (xlsx.Cell, string) {
	switch rapid.IntRange(0, 3).Draw(t, "cell") {
	case 0:
		s := rapid.String().Draw(t, "s")
		return xlsx.Str(s), xlsx.Sanitize(s)
	case 1:
		v := rapid.Int64().Draw(t, "i")
		return xlsx.Int(v), strconv.FormatInt(v, 10)
	case 2:
		v := rapid.Int64().Draw(t, "m")
		return xlsx.Money(v), strconv.FormatInt(v, 10)
	}
	return xlsx.Empty(), ""
}

// checkCells: các ô dữ liệu (từ dòng 2) đọc lại đúng want; ô ngoài want là ô trống hoặc chữ rỗng.
func checkCells(t *rapid.T, data []byte, want map[string]string) {
	b, err := xlsxtest.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if fp := b.FormulaParts(); len(fp) != 0 {
		t.Fatalf("có <f>: %v", fp)
	}
	rows, err := b.Rows(sheetPart)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rows[1:] {
		for _, c := range r.Cells {
			got[c.Ref] = c.Value
		}
	}
	for ref, v := range want {
		if got[ref] != v {
			t.Fatalf("%s: đọc lại %q, mong %q", ref, got[ref], v)
		}
	}
	for ref, v := range got {
		if _, ok := want[ref]; !ok && v != "" {
			t.Fatalf("%s: ô trống mà có %q", ref, v)
		}
	}
}
