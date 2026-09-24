package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/navyaraksha/imogi/internal/application/filebatch/preview"
)

func TestImportPreviewUsesMasterIdentityIndexAndWritesLocalArtifacts(t *testing.T) {
	_, sourceFile, _, _ := runtime.Caller(0)
	serverRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	config := preview.DefaultConfig()
	config.MasterFile = filepath.Join(serverRoot, "docs/dkm_master_employee.xlsx")
	config.PayrollFile = filepath.Join(serverRoot, "docs/salaries/01 JANUARI 2026/Salary 2026_01_07.xlsm")
	config.MasterTemplate = filepath.Join(serverRoot, "docs/import-templates/dkm-master.json")
	config.PayrollTemplate = filepath.Join(serverRoot, "docs/import-templates/dkm-rekap-payroll.json")
	config.OutputRoot = t.TempDir()
	if _, err := os.Stat(config.MasterFile); os.IsNotExist(err) {
		t.Skip("local employee fixture is not available")
	}
	if _, err := os.Stat(config.PayrollFile); os.IsNotExist(err) {
		t.Skip("local payroll fixture is not available")
	}

	result, err := preview.Run(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Ready {
		t.Fatal("fixture preview should be ready for import")
	}
	if result.Master.ValidRows != 341 || result.Master.InvalidRows != 0 {
		t.Fatalf("master result = %#v", result.Master)
	}
	if result.Payroll.ValidRows != 163 || result.Payroll.InvalidRows != 0 || result.Payroll.WarningRows != 1 {
		t.Fatalf("payroll result = %#v", result.Payroll)
	}
	for _, path := range []string{
		filepath.Join(result.RunDirectory, "manifest.json"),
		filepath.Join(result.RunDirectory, "result.json"),
		filepath.Join(result.RunDirectory, "master", "ready-import.ndjson"),
		filepath.Join(result.RunDirectory, "payroll", "ready-import.ndjson"),
		filepath.Join(result.RunDirectory, "payroll", "components.ndjson"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("artifact %s: %v", path, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(result.RunDirectory, "master", "ready-import.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "bank_account") || strings.Contains(string(data), "NAMA REKENING") {
		t.Fatal("master artifact contains a bank field")
	}
	var summary map[string]any
	if err := json.Unmarshal(mustReadFile(t, filepath.Join(result.RunDirectory, "payroll", "validation-summary.json")), &summary); err != nil {
		t.Fatal(err)
	}
	if summary["sheets"].([]any)[0] != "DKM Rekap" {
		t.Fatalf("payroll sheet summary = %#v", summary["sheets"])
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
