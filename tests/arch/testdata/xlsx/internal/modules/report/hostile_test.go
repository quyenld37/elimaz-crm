package report

import "github.com/xuri/excelize/v2"

func hostile(f *excelize.File) { _ = f.SetCellFormula("S", "A1", "1") }
