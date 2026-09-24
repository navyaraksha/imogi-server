package fileparser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParserReadsCSVRows(t *testing.T) {
	parser, err := New(100)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parser.Open(strings.NewReader("NIK,Nama\n1234567890123456,Ardianto\n"), "csv")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected header row")
	}
	header, err := rows.Columns()
	if err != nil || len(header) != 2 || header[0] != "NIK" {
		t.Fatalf("unexpected header: %#v, %v", header, err)
	}
	if !rows.Next() {
		t.Fatal("expected data row")
	}
	data, err := rows.Columns()
	if err != nil || data[1] != "Ardianto" {
		t.Fatalf("unexpected data: %#v, %v", data, err)
	}
	if rows.Next() {
		t.Fatal("unexpected extra row")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestParserListsWorkbookSheets(t *testing.T) {
	parser, err := New(1000)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "..", "..", "docs", "dkm_master_employee.xlsx")
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	workbook, err := parser.OpenWorkbook(input, "xlsx")
	if err != nil {
		t.Fatal(err)
	}
	defer workbook.Close()
	sheets := workbook.Sheets()
	if len(sheets) < 2 || sheets[0].Name != "master_employees" || sheets[1].Name != "emp_num_history" {
		t.Fatalf("unexpected workbook sheets: %#v", sheets)
	}
	rows, err := workbook.OpenSheet("master_employees")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected master employee header")
	}
	header, err := rows.Columns()
	if err != nil || len(header) == 0 || header[0] != "KTP" {
		t.Fatalf("unexpected master header: %#v, %v", header, err)
	}
}

func TestParserReadsJSONObjects(t *testing.T) {
	parser, err := New(100)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parser.Open(strings.NewReader(`[{"nama":"Ardianto","nik":"1234567890123456"}]`), "json")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected JSON header row")
	}
	header, _ := rows.Columns()
	if len(header) != 2 || header[0] != "nama" || header[1] != "nik" {
		t.Fatalf("unexpected sorted JSON header: %#v", header)
	}
}
