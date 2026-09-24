package tabular

import "io"

// RowReader is the small boundary shared by import processors and format
// adapters. It keeps spreadsheet libraries out of the application layer.
type RowReader interface {
	Next() bool
	Columns() ([]string, error)
	Err() error
	Close() error
}

type Parser interface {
	Open(io.Reader, string) (RowReader, error)
}

// WorkbookParser is the richer boundary for formats that contain multiple
// sheets. CSV and JSON adapters expose one virtual sheet.
type WorkbookParser interface {
	Parser
	OpenWorkbook(io.Reader, string) (Workbook, error)
}

type SheetInfo struct {
	Name       string
	Index      int
	MaxRows    int
	MaxColumns int
	Hidden     bool
}

type Workbook interface {
	Sheets() []SheetInfo
	OpenSheet(string) (RowReader, error)
	Close() error
}
