package report

import "github.com/xuri/excelize/v2"

func Bad(f *excelize.File) {
	_ = f.SetCellFormula("S", "A1", "SUM(B1)")
}
