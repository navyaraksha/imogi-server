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
