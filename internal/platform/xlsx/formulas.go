package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// maxCellSlots là trần tổng "số ô phải dựng" trên mọi phần XML của tệp: với mỗi dòng có ô, excelize trả một mảng dài
// bằng số cột của ô cuối (lấp chỗ trống bằng ""), kể cả dòng mà Read coi là trống và không đếm vào MaxRows. Mẫu thật
// 20.000 dòng × 30 cột là 600.000; một tệp nén 5 MiB có ô ở cột XFD trên hàng triệu dòng là hàng chục tỷ.
const maxCellSlots = 10_000_000

// workbookPart là đường dẫn sổ duy nhất Read nhận (Excel, LibreOffice, Google Sheets đều ghi đúng chỗ này).
const workbookPart = "xl/workbook.xml"

// relNamespaces: namespace của thuộc tính r:id (Open XML transitional và strict).
func relNamespaces() []string {
	return []string{
		"http://schemas.openxmlformats.org/officeDocument/2006/relationships",
		"http://purl.oclc.org/ooxml/officeDocument/relationships",
	}
}

// sheetRef là trang đầu của sổ: tên (để đối chiếu với excelize) và đường dẫn phần trong zip.
type sheetRef struct{ name, path string }

// scanBook chạy trước excelize: đếm số ô phải dựng trên mọi phần của zip (quá maxCellSlots → ErrTooLarge, dù excelize
// đọc phần nào), tìm trang đầu theo đúng quy tắc của excelize (từ chối khi cấu trúc khác thường, để hai bên không đọc
// hai trang khác nhau) và mọi ô có phần tử <f> của trang đó (số dòng → các số cột, đánh số từ 1).
//
// Không dùng excelize.GetCellFormula: với công thức dùng chung không có ô gốc, mỗi lần gọi quét lại cả trang, nên một
// tệp nhiều ô như vậy tốn thời gian bình phương số ô (đo ở Task M1-38: 20.000 ô mất 1 s).
func scanBook(zr *zip.Reader) (sheetRef, map[int][]int, error) {
	slots := 0
	for _, f := range zr.File { // mọi phần, không theo đuôi: trang có thể mang tên bất kỳ (excelize nạp mọi phần)
		if err := countSlots(f, &slots); err != nil {
			return sheetRef{}, nil, err
		}
	}
	sheet, err := firstSheet(zr)
	if err != nil {
		return sheetRef{}, nil, err
	}
	f := findPart(zr, sheet.path)
	if f == nil {
		return sheetRef{}, nil, fmt.Errorf("%w: thiếu trang đầu", ErrNotXlsx)
	}
	sc := sheetScan{formulas: map[int][]int{}}
	if err := sc.scan(f); err != nil {
		return sheetRef{}, nil, broken("quét công thức")
	}
	return sheet, sc.formulas, nil
}

// countSlots cộng số ô phải dựng của phần f vào *slots. XML hỏng: dừng ở chỗ hỏng (excelize cũng dừng ở đó).
func countSlots(f *zip.File, slots *int) error {
	sc := sheetScan{slots: *slots}
	err := sc.scan(f)
	*slots = sc.slots
	if errors.Is(err, ErrTooLarge) {
		return ErrTooLarge
	}
	return nil
}

// sheetScan là trạng thái của lượt quét một phần XML: vị trí dòng, ô đang đọc; tổng ô phải dựng; ô công thức
// (formulas nil: không ghi).
type sheetScan struct {
	formulas       map[int][]int
	row, col       int
	inCell, marked bool
	slots, rowMax  int // tổng ô phải dựng; cột lớn nhất của dòng đang đọc
}

func (sc *sheetScan) scan(f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("mở phần: %w", err)
	}
	defer func() { _ = rc.Close() }()
	d := xml.NewDecoder(rc)
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("đọc XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			sc.start(t)
		case xml.EndElement:
			if err := sc.end(t); err != nil {
				return err
			}
		}
	}
}

func (sc *sheetScan) start(t xml.StartElement) {
	switch t.Name.Local {
	case "row":
		sc.row, sc.col, sc.rowMax = position(t, "r", sc.row+1, strconv.Atoi), 0, 0
	case "c":
		sc.col = position(t, "r", sc.col+1, columnOf)
		sc.inCell, sc.marked, sc.rowMax = true, false, max(sc.rowMax, sc.col)
	case "f":
		if sc.formulas != nil && sc.inCell && !sc.marked {
			sc.formulas[sc.row] = append(sc.formulas[sc.row], sc.col)
			sc.marked = true
		}
	}
}

func (sc *sheetScan) end(t xml.EndElement) error {
	switch t.Name.Local {
	case "c":
		sc.inCell = false
	case "row":
		if sc.slots += sc.rowMax; sc.slots > maxCellSlots {
			return ErrTooLarge
		}
		sc.rowMax = 0
	}
	return nil
}

// position đọc thuộc tính attr của phần tử bằng parse; không có hay sai thì lấy next (cách excelize đánh số).
func position(t xml.StartElement, attr string, next int, parse func(string) (int, error)) int {
	for _, a := range t.Attr {
		if a.Name.Local == attr {
			if n, err := parse(a.Value); err == nil && n > 0 {
				return n
			}
		}
	}
	return next
}

// columnOf: số cột của ô ref ("C4" → 3).
func columnOf(ref string) (int, error) {
	c, _, err := excelize.CellNameToCoordinates(ref)
	if err != nil {
		return 0, fmt.Errorf("cột của ô: %w", err)
	}
	return c, nil
}

// firstSheet tìm trang đầu như excelize: sổ là đích quan hệ officeDocument đầu tiên của _rels/.rels (phải là
// xl/workbook.xml); trang đầu là thẻ <sheet> đầu trong <sheets>, đường dẫn theo quan hệ r:id trong
// xl/_rels/workbook.xml.rels (tương đối với xl/, hoặc tuyệt đối khi mở bằng "/"). Từ chối những chỗ excelize và gói
// có thể chọn khác nhau: tên trang trùng, Id quan hệ của trang đầu xuất hiện hơn một lần (excelize lấy cái cuối), đích
// có "\" (excelize và path.Clean xử lý khác nhau).
func firstSheet(zr *zip.Reader) (sheetRef, error) {
	notFound := fmt.Errorf("%w: không tìm thấy trang đầu", ErrNotXlsx)
	if err := checkWorkbookPath(zr); err != nil {
		return sheetRef{}, err
	}
	sheets, err := elems(zr, workbookPart, []string{"workbook", "sheets"}, "sheet")
	if err != nil || len(sheets) == 0 {
		return sheetRef{}, notFound
	}
	names := map[string]bool{}
	for _, t := range sheets {
		n := strings.ToLower(attr(t, "name", nil))
		if n == "" || names[n] {
			return sheetRef{}, fmt.Errorf("%w: tên trang rỗng hoặc trùng", ErrNotXlsx)
		}
		names[n] = true
	}
	name, rid := attr(sheets[0], "name", nil), relID(sheets[0])
	wbRels, err := elems(zr, "xl/_rels/workbook.xml.rels", []string{"Relationships"}, "Relationship")
	if err != nil || rid == "" {
		return sheetRef{}, notFound
	}
	var targets []string
	for _, t := range wbRels {
		if attr(t, "Id", nil) == rid {
			targets = append(targets, attr(t, "Target", nil))
		}
	}
	if len(targets) != 1 || targets[0] == "" || strings.Contains(targets[0], `\`) {
		return sheetRef{}, notFound
	}
	if target := targets[0]; strings.HasPrefix(target, "/") {
		return sheetRef{name: name, path: strings.TrimPrefix(path.Clean(target), "/")}, nil
	}
	return sheetRef{name: name, path: strings.TrimPrefix(path.Clean("xl/"+targets[0]), "/")}, nil
}

// checkWorkbookPath: đích quan hệ officeDocument đầu tiên (con trực tiếp) của _rels/.rels phải là workbookPart.
func checkWorkbookPath(zr *zip.Reader) error {
	rels, err := elems(zr, "_rels/.rels", []string{"Relationships"}, "Relationship")
	if err != nil {
		return err
	}
	for _, t := range rels {
		if slices.Contains(relNamespaces(), strings.TrimSuffix(attr(t, "Type", nil), "/officeDocument")) {
			if strings.TrimPrefix(attr(t, "Target", nil), "/") == workbookPart {
				return nil
			}
			break
		}
	}
	return fmt.Errorf("%w: sổ không ở %s", ErrNotXlsx, workbookPart)
}

// relID trả r:id của thẻ <sheet>; rỗng khi không có hoặc có id của cả namespace transitional lẫn strict (excelize đổi
// strict sang transitional rồi giữ cái cuối, gói lấy cái đầu, nên từ chối).
func relID(t xml.StartElement) string {
	ids := slices.DeleteFunc(slices.Clone(t.Attr), func(a xml.Attr) bool {
		return a.Name.Local != "id" || !slices.Contains(relNamespaces(), a.Name.Space)
	})
	if len(ids) != 1 {
		return ""
	}
	return ids[0].Value
}

// attr trả giá trị thuộc tính local (spaces khác nil: chỉ thuộc tính thuộc một trong các namespace đó).
func attr(t xml.StartElement, local string, spaces []string) string {
	for _, a := range t.Attr {
		if a.Name.Local == local && (spaces == nil || slices.Contains(spaces, a.Name.Space)) {
			return a.Value
		}
	}
	return ""
}

// elems trả mọi phần tử tên local nằm đúng dưới chuỗi thẻ under tính từ gốc (ví dụ ["workbook", "sheets"]): chỉ
// con trực tiếp, đúng như encoding/xml giải vào kiểu của excelize; thẻ cùng tên lồng ở chỗ khác bị bỏ như excelize
// bỏ. Phần nhỏ (sổ, quan hệ) nên đọc hết; XML hỏng là ErrNotXlsx.
func elems(zr *zip.Reader, part string, under []string, local string) ([]xml.StartElement, error) {
	f := findPart(zr, part)
	if f == nil {
		return nil, fmt.Errorf("%w: thiếu %s", ErrNotXlsx, part)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, broken("mở " + part)
	}
	defer func() { _ = rc.Close() }()
	d := xml.NewDecoder(rc)
	var (
		stack []string // tên các thẻ đang mở, từ gốc
		out   []xml.StartElement
	)
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, broken("đọc " + part)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == local && slices.Equal(stack, under) {
				out = append(out, t.Copy())
			}
			stack = append(stack, t.Name.Local)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
}

// partName là tên phần như excelize dùng làm khóa ("\" đổi thành "/").
func partName(name string) string { return strings.ReplaceAll(name, `\`, "/") }

// findPart trả phần có tên (theo partName) đúng bằng name; checkZip đã bảo đảm không có hai phần trùng tên.
func findPart(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if partName(f.Name) == name {
			return f
		}
	}
	return nil
}
