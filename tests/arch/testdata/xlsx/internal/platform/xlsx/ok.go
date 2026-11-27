package xlsx

import "github.com/xuri/excelize/v2"

func OK(f *excelize.File) error { return f.SetCellStr("S", "A1", "x") }
