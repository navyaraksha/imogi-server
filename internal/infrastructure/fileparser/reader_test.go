package fileparser

import (
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
