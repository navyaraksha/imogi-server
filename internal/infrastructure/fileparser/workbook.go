package fileparser

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/extrame/xls"
	"github.com/navyaraksha/imogi/internal/platform/tabular"
	"github.com/xuri/excelize/v2"
)

func (parser *Parser) OpenWorkbook(input io.Reader, extension string) (tabular.Workbook, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}
	switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(extension)), ".") {
	case "csv":
		return newCSVWorkbook(data), nil
	case "json":
		rows, err := newJSONRows(data)
		if err != nil {
			return nil, err
		}
		return &memoryWorkbook{
			rows:   map[string]tabular.RowReader{"Sheet1": rows},
			sheets: []tabular.SheetInfo{{Name: "Sheet1", Index: 0}},
		}, nil
	case "xlsx", "xlsm":
		return newExcelWorkbook(data, parser.MaxRows)
	case "xls", "xlm":
		return newXLSWorkbook(data, parser.MaxRows)
	default:
		return nil, fmt.Errorf("unsupported file extension %q", extension)
	}
}

type workbookRows struct {
	tabular.RowReader
	closeWorkbook func() error
}

func (rows *workbookRows) Close() error {
	rowErr := rows.RowReader.Close()
	workbookErr := rows.closeWorkbook()
	if rowErr != nil {
		return rowErr
	}
	return workbookErr
}

type memoryWorkbook struct {
	rows   map[string]tabular.RowReader
	sheets []tabular.SheetInfo
}

func (workbook *memoryWorkbook) Sheets() []tabular.SheetInfo {
	return append([]tabular.SheetInfo(nil), workbook.sheets...)
}

func (workbook *memoryWorkbook) OpenSheet(name string) (tabular.RowReader, error) {
	rows, ok := workbook.rows[name]
	if !ok {
		return nil, fmt.Errorf("sheet %q not found", name)
	}
	return rows, nil
}

func (workbook *memoryWorkbook) Close() error { return nil }

func newCSVWorkbook(data []byte) tabular.Workbook {
	return &memoryWorkbook{
		rows: map[string]tabular.RowReader{
			"Sheet1": &csvRows{reader: csv.NewReader(bytes.NewReader(data))},
		},
		sheets: []tabular.SheetInfo{{Name: "Sheet1", Index: 0}},
	}
}

type excelWorkbook struct {
	file    *excelize.File
	maxRows int
	sheets  []tabular.SheetInfo
}

func newExcelWorkbook(data []byte, maxRows int) (*excelWorkbook, error) {
	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	sheetNames := file.GetSheetList()
	sheets := make([]tabular.SheetInfo, 0, len(sheetNames))
	for index, name := range sheetNames {
		hidden, hiddenErr := file.GetSheetVisible(name)
		if hiddenErr != nil {
			_ = file.Close()
			return nil, hiddenErr
		}
		sheets = append(sheets, tabular.SheetInfo{Name: name, Index: index, Hidden: !hidden})
	}
	return &excelWorkbook{file: file, maxRows: maxRows, sheets: sheets}, nil
}

func (workbook *excelWorkbook) Sheets() []tabular.SheetInfo {
	return append([]tabular.SheetInfo(nil), workbook.sheets...)
}

func (workbook *excelWorkbook) OpenSheet(name string) (tabular.RowReader, error) {
	rows, err := workbook.file.Rows(name)
	if err != nil {
		return nil, err
	}
	return &limitedRows{rows: &excelRows{rows: rows}, maxRows: workbook.maxRows}, nil
}

func (workbook *excelWorkbook) Close() error { return workbook.file.Close() }

type xlsWorkbook struct {
	workbook *xls.WorkBook
	maxRows  int
	sheets   []tabular.SheetInfo
}

func newXLSWorkbook(data []byte, maxRows int) (*xlsWorkbook, error) {
	workbook, err := xls.OpenReader(bytes.NewReader(data), "utf-8")
	if err != nil {
		return nil, err
	}
	sheets := make([]tabular.SheetInfo, 0, workbook.NumSheets())
	for index := 0; index < workbook.NumSheets(); index++ {
		sheet := workbook.GetSheet(index)
		if sheet == nil {
			continue
		}
		sheets = append(sheets, tabular.SheetInfo{Name: sheet.Name, Index: index, MaxRows: int(sheet.MaxRow) + 1})
	}
	return &xlsWorkbook{workbook: workbook, maxRows: maxRows, sheets: sheets}, nil
}

func (workbook *xlsWorkbook) Sheets() []tabular.SheetInfo {
	return append([]tabular.SheetInfo(nil), workbook.sheets...)
}

func (workbook *xlsWorkbook) OpenSheet(name string) (tabular.RowReader, error) {
	for index := range workbook.sheets {
		if workbook.sheets[index].Name == name {
			return newXLSRowsFromSheet(workbook.workbook.GetSheet(workbook.sheets[index].Index), workbook.maxRows), nil
		}
	}
	return nil, fmt.Errorf("sheet %q not found", name)
}

func (workbook *xlsWorkbook) Close() error { return nil }

type limitedRows struct {
	rows    tabular.RowReader
	maxRows int
	read    int
}

func (rows *limitedRows) Next() bool {
	if rows.maxRows > 0 && rows.read >= rows.maxRows {
		return false
	}
	if !rows.rows.Next() {
		return false
	}
	rows.read++
	return true
}

func (rows *limitedRows) Columns() ([]string, error) { return rows.rows.Columns() }
func (rows *limitedRows) Err() error                 { return rows.rows.Err() }
func (rows *limitedRows) Close() error               { return rows.rows.Close() }

func newXLSRowsFromSheet(sheet *xls.WorkSheet, maxRows int) tabular.RowReader {
	if sheet == nil {
		return &xlsRows{err: errors.New("sheet is not available")}
	}
	rowCount := int(sheet.MaxRow) + 1
	if maxRows > 0 && rowCount > maxRows {
		rowCount = maxRows
	}
	rows := make([][]string, 0, rowCount)
	for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
		row := sheet.Row(rowIndex)
		if row == nil {
			rows = append(rows, nil)
			continue
		}
		values := make([]string, row.LastCol()+1)
		for column := row.FirstCol(); column <= row.LastCol(); column++ {
			values[column] = row.Col(column)
		}
		rows = append(rows, values)
	}
	return &xlsRows{rows: rows}
}
