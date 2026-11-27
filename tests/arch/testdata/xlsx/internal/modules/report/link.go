package report

type book interface {
	SetCellHyperLink(sheet, cell, link, linkType string) error
	AddVBAProject(b []byte) error
}

func Bad(b book) {
	_ = b.SetCellHyperLink("S", "A1", "http://x", "External")
	_ = b.AddVBAProject(nil)
}
