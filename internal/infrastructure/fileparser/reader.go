package fileparser

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/extrame/xls"
	"github.com/navyaraksha/imogi/internal/platform/tabular"
	"github.com/xuri/excelize/v2"
)

type Parser struct {
	MaxRows int
}

func New(maxRows int) (*Parser, error) {
	if maxRows <= 0 {
		return nil, errors.New("max rows must be positive")
	}
	return &Parser{MaxRows: maxRows}, nil
}

func (parser *Parser) Open(input io.Reader, extension string) (tabular.RowReader, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}
	switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(extension)), ".") {
	case "csv":
		return &csvRows{reader: csv.NewReader(bytes.NewReader(data))}, nil
	case "json":
		return newJSONRows(data)
	case "xlsx", "xlsm":
		return newExcelRows(data)
	case "xls":
		return newXLSRows(data, parser.MaxRows)
	default:
		return nil, fmt.Errorf("unsupported file extension %q", extension)
	}
}

type csvRows struct {
	reader *csv.Reader
	row    []string
	err    error
}

func (rows *csvRows) Next() bool {
	rows.row, rows.err = rows.reader.Read()
	return rows.err == nil
}
func (rows *csvRows) Columns() ([]string, error) { return rows.row, nil }
func (rows *csvRows) Err() error {
	if errors.Is(rows.err, io.EOF) {
		return nil
	}
	return rows.err
}
func (rows *csvRows) Close() error { return nil }

type jsonRows struct {
	rows  [][]string
	index int
}

func newJSONRows(data []byte) (*jsonRows, error) {
	var raw []map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("json import must be an array of objects: %w", err)
	}
	if len(raw) == 0 {
		return &jsonRows{}, nil
	}
	keys := make([]string, 0, len(raw[0]))
	for key := range raw[0] {
		keys = append(keys, key)
	}
	for index := 1; index < len(raw); index++ {
		for key := range raw[index] {
			if !contains(keys, key) {
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	rows := make([][]string, 0, len(raw)+1)
	rows = append(rows, keys)
	for _, item := range raw {
		values := make([]string, len(keys))
		for index, key := range keys {
			if value, ok := item[key]; ok {
				values[index] = fmt.Sprint(value)
			}
		}
		rows = append(rows, values)
	}
	return &jsonRows{rows: rows}, nil
}
func (rows *jsonRows) Next() bool { rows.index++; return rows.index <= len(rows.rows) }
func (rows *jsonRows) Columns() ([]string, error) {
	if rows.index == 0 || rows.index > len(rows.rows) {
		return nil, errors.New("row is not selected")
	}
	return rows.rows[rows.index-1], nil
}
func (rows *jsonRows) Err() error   { return nil }
func (rows *jsonRows) Close() error { return nil }

type excelRows struct {
	rows  *excelize.Rows
	close func() error
	row   []string
	err   error
}

func newExcelRows(data []byte) (*excelRows, error) {
	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	sheet := file.GetSheetName(0)
	rows, err := file.Rows(sheet)
	if err != nil {
		file.Close()
		return nil, err
	}
	return &excelRows{rows: rows, close: file.Close}, nil
}
func (rows *excelRows) Next() bool {
	if !rows.rows.Next() {
		rows.err = rows.rows.Error()
		return false
	}
	rows.row, rows.err = rows.rows.Columns()
	return rows.err == nil
}
func (rows *excelRows) Columns() ([]string, error) { return rows.row, rows.err }
func (rows *excelRows) Err() error                 { return rows.err }
func (rows *excelRows) Close() error {
	if err := rows.rows.Close(); err != nil {
		return err
	}
	return rows.close()
}

type xlsRows struct {
	rows  [][]string
	index int
}

func newXLSRows(data []byte, maxRows int) (*xlsRows, error) {
	workbook, err := xls.OpenReader(bytes.NewReader(data), "utf-8")
	if err != nil {
		return nil, err
	}
	return &xlsRows{rows: workbook.ReadAllCells(maxRows)}, nil
}
func (rows *xlsRows) Next() bool { rows.index++; return rows.index <= len(rows.rows) }
func (rows *xlsRows) Columns() ([]string, error) {
	if rows.index == 0 || rows.index > len(rows.rows) {
		return nil, errors.New("row is not selected")
	}
	return rows.rows[rows.index-1], nil
}
func (rows *xlsRows) Err() error   { return nil }
func (rows *xlsRows) Close() error { return nil }

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
