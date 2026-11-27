// Package xlsxtest đọc lại tệp .xlsx trong test bằng archive/zip và encoding/xml, độc lập với excelize (kế hoạch M6,
// Task M6-13): test của bộ ghi và của báo cáo (M6-14, 15) kiểm nội dung thật của tệp — không phần tử <f>, kiểu ô,
// định dạng số — thay vì tin vào cách excelize đọc lại chính tệp nó ghi.
//
// Khi nào không dùng: không dùng trong mã chạy thật (không giới hạn kích thước, đọc cả tệp vào bộ nhớ); đọc tệp người
// dùng tải lên thì dùng xlsx.Read.
package xlsxtest

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Cell là một ô của trang: Ref (vd "B3"), Type là thuộc tính t ("n", "s", "inlineStr", "str"… — rỗng là số), Style
// là chỉ số s trong cellXfs, Value là chữ của <v> hoặc chữ của <is> (đã giải mã _xHHHH_), Formula là ô có phần tử <f>.
type Cell struct {
	Ref     string
	Type    string
	Style   int
	Value   string
	Formula bool
}

// Row là một dòng: N là số dòng (thuộc tính r), Cells theo thứ tự trong tệp.
type Row struct {
	N     int
	Cells []Cell
}

// Style là kiểu của một ô theo cellXfs: mã định dạng số (tự định nghĩa hoặc dựng sẵn) và chữ đậm, nghiêng.
type Style struct {
	NumFmt string
	Bold   bool
	Italic bool
}

// Book là mọi phần của một tệp .xlsx, giữ trong bộ nhớ.
type Book struct {
	names []string
	parts map[string][]byte
}

// Read đọc mọi phần của tệp b. Lỗi khi b không phải zip hay một phần không giải nén được.
func Read(b []byte) (*Book, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("mở zip: %w", err)
	}
	book := &Book{parts: map[string][]byte{}}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("mở phần %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("đọc phần %s: %w", f.Name, err)
		}
		book.names = append(book.names, f.Name)
		book.parts[f.Name] = data
	}
	return book, nil
}

// Parts trả tên mọi phần theo thứ tự trong zip.
func (b *Book) Parts() []string { return append([]string(nil), b.names...) }

// Part trả nội dung một phần.
func (b *Book) Part(name string) ([]byte, bool) {
	p, ok := b.parts[name]
	return p, ok
}

// FormulaParts trả tên các phần XML có ít nhất một phần tử tên "f" (công thức), ở bất kỳ namespace nào. Phần không
// phải XML hợp lệ cũng được trả (kèm lỗi trong chuỗi) để test không bỏ sót.
func (b *Book) FormulaParts() []string {
	var out []string
	for _, n := range b.names {
		if !strings.HasSuffix(n, ".xml") && !strings.HasSuffix(n, ".rels") {
			continue
		}
		has, err := hasElement(b.parts[n], "f")
		switch {
		case err != nil:
			out = append(out, n+" (XML hỏng: "+err.Error()+")")
		case has:
			out = append(out, n)
		}
	}
	return out
}

func hasElement(data []byte, local string) (bool, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err //nolint:wrapcheck // lỗi XML chỉ để hiện trong test
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == local {
			return true, nil
		}
	}
}

// xmlCell là một phần tử <c> của sheetData.
type xmlCell struct {
	R  string    `xml:"r,attr"`
	T  string    `xml:"t,attr"`
	S  int       `xml:"s,attr"`
	F  *struct{} `xml:"f"`
	V  string    `xml:"v"`
	IS *struct {
		T []string `xml:"t"`
		R []struct {
			T string `xml:"t"`
		} `xml:"r"`
	} `xml:"is"`
}

type xmlRow struct {
	R     int       `xml:"r,attr"`
	Cells []xmlCell `xml:"c"`
}

// Rows đọc sheetData của phần trang part (vd "xl/worksheets/sheet1.xml") bằng encoding/xml.
func (b *Book) Rows(part string) ([]Row, error) {
	data, ok := b.parts[part]
	if !ok {
		return nil, fmt.Errorf("không có phần %s", part)
	}
	var ws struct {
		Rows []xmlRow `xml:"sheetData>row"`
	}
	if err := xml.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("đọc %s: %w", part, err)
	}
	out := make([]Row, 0, len(ws.Rows))
	for _, r := range ws.Rows {
		row := Row{N: r.R}
		for _, c := range r.Cells {
			v := c.V
			if c.IS != nil {
				// Mỗi phần tử <t> giải mã riêng (ST_Xstring của từng phần tử), rồi nối lại.
				var sb strings.Builder
				for _, t := range c.IS.T {
					sb.WriteString(DecodeXstring(t))
				}
				for _, run := range c.IS.R {
					sb.WriteString(DecodeXstring(run.T))
				}
				v = sb.String()
			}
			row.Cells = append(row.Cells, Cell{Ref: c.R, Type: c.T, Style: c.S, Value: v, Formula: c.F != nil})
		}
		out = append(out, row)
	}
	return out, nil
}

// builtinNumFmts: các mã định dạng dựng sẵn mà test cần đối chiếu (ECMA-376 phần 1, 18.8.30).
func builtinNumFmts() map[int]string {
	return map[int]string{0: "General", 1: "0", 2: "0.00", 3: "#,##0", 4: "#,##0.00", 14: "mm-dd-yy", 22: "m/d/yy h:mm"}
}

// Style trả kiểu của chỉ số i trong cellXfs của xl/styles.xml.
func (b *Book) Style(i int) (Style, error) {
	data, ok := b.parts["xl/styles.xml"]
	if !ok {
		return Style{}, errors.New("không có xl/styles.xml")
	}
	var st struct {
		NumFmts []struct {
			ID   int    `xml:"numFmtId,attr"`
			Code string `xml:"formatCode,attr"`
		} `xml:"numFmts>numFmt"`
		Fonts []struct {
			B *struct{} `xml:"b"`
			I *struct{} `xml:"i"`
		} `xml:"fonts>font"`
		Xfs []struct {
			NumFmtID int `xml:"numFmtId,attr"`
			FontID   int `xml:"fontId,attr"`
		} `xml:"cellXfs>xf"`
	}
	if err := xml.Unmarshal(data, &st); err != nil {
		return Style{}, fmt.Errorf("đọc styles: %w", err)
	}
	if i < 0 || i >= len(st.Xfs) {
		return Style{}, fmt.Errorf("không có kiểu %d", i)
	}
	xf := st.Xfs[i]
	out := Style{NumFmt: builtinNumFmts()[xf.NumFmtID]}
	for _, nf := range st.NumFmts {
		if nf.ID == xf.NumFmtID {
			out.NumFmt = nf.Code
		}
	}
	if xf.FontID >= 0 && xf.FontID < len(st.Fonts) {
		out.Bold = st.Fonts[xf.FontID].B != nil
		out.Italic = st.Fonts[xf.FontID].I != nil
	}
	return out, nil
}

// xstringEscape: chuỗi thoát _xHHHH_ của kiểu ST_Xstring (ECMA-376 phần 1, 22.9.2.19).
var xstringEscape = regexp.MustCompile(`_x[0-9A-Fa-f]{4}_`)

// DecodeXstring giải mã _xHHHH_ như Excel đọc ô chữ: mỗi dãy là một ký tự (_x005F_ là "_"), quét từ trái sang phải.
// Dãy trỏ tới nửa cặp UTF-16 giữ nguyên chữ (excelize cũng không thoát dãy đó khi ghi).
func DecodeXstring(s string) string {
	if !strings.Contains(s, "_x") {
		return s
	}
	return xstringEscape.ReplaceAllStringFunc(s, func(m string) string {
		var r rune
		for _, c := range []byte(m[2:6]) {
			r = r*16 + hexValue(c)
		}
		if r >= 0xD800 && r < 0xE000 {
			return m
		}
		return string(r)
	})
}

// hexValue là giá trị của một chữ số thập lục phân (biểu thức xstringEscape đã bảo đảm c hợp lệ).
func hexValue(c byte) rune {
	switch {
	case c >= 'a':
		return rune(c-'a') + 10
	case c >= 'A':
		return rune(c-'A') + 10
	}
	return rune(c - '0')
}
