// Package preview contains the local, non-committing import verification flow.
// It composes the production parser and validator with an in-memory repository
// so fixture validation never changes tenant data.
package preview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	appfilebatch "github.com/navyaraksha/imogi/internal/application/filebatch"
	filedomain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	fileparser "github.com/navyaraksha/imogi/internal/infrastructure/fileparser"
	objectstorage "github.com/navyaraksha/imogi/internal/infrastructure/objectstorage"
	"github.com/navyaraksha/imogi/internal/platform/clock"
)

const (
	defaultMasterFile    = "docs/dkm_master_employee.xlsx"
	defaultPayrollFile   = "docs/salaries/01 JANUARI 2026/Salary 2026_01_07.xlsm"
	defaultMasterConfig  = "docs/import-templates/dkm-master.json"
	defaultPayrollConfig = "docs/import-templates/dkm-rekap-payroll.json"
)

var (
	previewTenantID  = organization.TenantID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-111111111111"))
	previewCompanyID = organization.CompanyID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-222222222222"))
	previewUserID    = identitydomain.UserID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-333333333333"))
)

type Config struct {
	MasterFile      string
	PayrollFile     string
	MasterTemplate  string
	PayrollTemplate string
	OutputRoot      string
	TaxYear         int
	TaxMonth        int
	CoverageFrom    time.Time
	CoverageTo      time.Time
	PayDate         *time.Time
	KeepInput       bool
	MaxRows         int
}

func DefaultConfig() Config {
	return Config{
		MasterFile:      defaultMasterFile,
		PayrollFile:     defaultPayrollFile,
		MasterTemplate:  defaultMasterConfig,
		PayrollTemplate: defaultPayrollConfig,
		OutputRoot:      ".tmp/import-preview",
		TaxYear:         2026,
		TaxMonth:        1,
		CoverageFrom:    time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		CoverageTo:      time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC),
		MaxRows:         100000,
	}
}

func BindFlags(flags *flag.FlagSet, config *Config) {
	flags.StringVar(&config.MasterFile, "master", config.MasterFile, "employee master workbook")
	flags.StringVar(&config.PayrollFile, "payroll", config.PayrollFile, "payroll workbook")
	flags.StringVar(&config.MasterTemplate, "master-template", config.MasterTemplate, "master template JSON")
	flags.StringVar(&config.PayrollTemplate, "payroll-template", config.PayrollTemplate, "payroll template JSON")
	flags.StringVar(&config.OutputRoot, "out", config.OutputRoot, "local preview output directory")
	flags.IntVar(&config.TaxYear, "tax-year", config.TaxYear, "payroll tax year")
	flags.IntVar(&config.TaxMonth, "tax-month", config.TaxMonth, "payroll tax month")
	flags.Func("coverage-from", "payroll coverage start date (YYYY-MM-DD)", func(value string) error {
		date, err := time.Parse("2006-01-02", value)
		if err != nil {
			return err
		}
		config.CoverageFrom = date.UTC()
		return nil
	})
	flags.Func("coverage-to", "payroll coverage end date (YYYY-MM-DD)", func(value string) error {
		date, err := time.Parse("2006-01-02", value)
		if err != nil {
			return err
		}
		config.CoverageTo = date.UTC()
		return nil
	})
	flags.BoolVar(&config.KeepInput, "keep-input", false, "keep uploaded source files under the preview directory")
	flags.IntVar(&config.MaxRows, "max-rows", config.MaxRows, "maximum logical rows per workbook")
}

type Result struct {
	RunDirectory string      `json:"runDirectory"`
	Master       BatchResult `json:"master"`
	Payroll      BatchResult `json:"payroll"`
	Ready        bool        `json:"ready"`
	CreatedAt    time.Time   `json:"createdAt"`
}

type BatchResult struct {
	BatchID     string `json:"batchId"`
	Status      string `json:"status"`
	TotalRows   int    `json:"totalRows"`
	ValidRows   int    `json:"validRows"`
	InvalidRows int    `json:"invalidRows"`
	WarningRows int    `json:"warningRows"`
	ArtifactDir string `json:"artifactDir"`
}

func Run(ctx context.Context, config Config) (Result, error) {
	if err := validateConfig(config); err != nil {
		return Result{}, err
	}

	masterTemplate, err := os.ReadFile(config.MasterTemplate)
	if err != nil {
		return Result{}, fmt.Errorf("read master template: %w", err)
	}
	payrollTemplate, err := os.ReadFile(config.PayrollTemplate)
	if err != nil {
		return Result{}, fmt.Errorf("read payroll template: %w", err)
	}
	masterData, err := os.ReadFile(config.MasterFile)
	if err != nil {
		return Result{}, fmt.Errorf("read master workbook: %w", err)
	}
	payrollData, err := os.ReadFile(config.PayrollFile)
	if err != nil {
		return Result{}, fmt.Errorf("read payroll workbook: %w", err)
	}

	runDirectory := filepath.Join(config.OutputRoot, time.Now().UTC().Format("20060102-150405.000000000"))
	if err := os.MkdirAll(runDirectory, 0o700); err != nil {
		return Result{}, fmt.Errorf("create preview directory: %w", err)
	}
	storage, err := objectstorage.NewFilesystem(filepath.Join(runDirectory, "storage"))
	if err != nil {
		return Result{}, err
	}
	repository := newMemoryRepository()
	parser, err := fileparser.New(config.MaxRows)
	if err != nil {
		return Result{}, err
	}
	validator, err := appfilebatch.NewValidator(repository, storage, parser, clock.System{}, config.MaxRows)
	if err != nil {
		return Result{}, err
	}

	masterBatch, err := createPreviewBatch(ctx, repository, storage, previewInput{
		Data:          masterData,
		Filename:      filepath.Base(config.MasterFile),
		Extension:     "xlsx",
		Operation:     filedomain.OperationEmployeeMaster,
		Configuration: masterTemplate,
	})
	if err != nil {
		return Result{}, err
	}
	if err := validator.Validate(ctx, masterBatch.ID); err != nil {
		return Result{}, fmt.Errorf("validate master: %w", err)
	}
	masterResult, err := mirrorArtifacts(ctx, repository, storage, masterBatch.ID, filepath.Join(runDirectory, "master"))
	if err != nil {
		return Result{}, err
	}

	matcher, err := buildIdentityMatcher(repository, masterBatch.ID)
	if err != nil {
		return Result{}, fmt.Errorf("build identity index: %w", err)
	}
	validator.SetIdentityMatcher(matcher)

	payrollBatch, err := createPreviewBatch(ctx, repository, storage, previewInput{
		Data:          payrollData,
		Filename:      filepath.Base(config.PayrollFile),
		Extension:     "xlsm",
		Operation:     filedomain.OperationPayrollLedger,
		Configuration: payrollTemplate,
		PayrollContext: &filedomain.PayrollImportContext{
			TaxYear:      config.TaxYear,
			TaxMonth:     config.TaxMonth,
			CoverageFrom: config.CoverageFrom,
			CoverageTo:   config.CoverageTo,
			PayDate:      config.PayDate,
			RunType:      "regular",
		},
	})
	if err != nil {
		return Result{}, err
	}
	if err := validator.Validate(ctx, payrollBatch.ID); err != nil {
		return Result{}, fmt.Errorf("validate payroll: %w", err)
	}
	payrollResult, err := mirrorArtifacts(ctx, repository, storage, payrollBatch.ID, filepath.Join(runDirectory, "payroll"))
	if err != nil {
		return Result{}, err
	}
	if err := writePayrollComponents(ctx, repository, storage, payrollBatch.ID, filepath.Join(runDirectory, "payroll", "components.ndjson")); err != nil {
		return Result{}, err
	}

	if !config.KeepInput {
		_ = storage.Delete(ctx, repository.files[masterBatch.InputFileID].ObjectKey)
		_ = storage.Delete(ctx, repository.files[payrollBatch.InputFileID].ObjectKey)
	}

	result := Result{
		RunDirectory: runDirectory,
		Master:       masterResult,
		Payroll:      payrollResult,
		Ready:        masterResult.InvalidRows == 0 && payrollResult.InvalidRows == 0,
		CreatedAt:    time.Now().UTC(),
	}
	if err := writeJSON(filepath.Join(runDirectory, "result.json"), result); err != nil {
		return Result{}, err
	}
	if err := writeJSON(filepath.Join(runDirectory, "manifest.json"), manifest{
		MasterFile:    config.MasterFile,
		PayrollFile:   config.PayrollFile,
		MasterSHA256:  sha256Hex(masterData),
		PayrollSHA256: sha256Hex(payrollData),
		TaxYear:       config.TaxYear,
		TaxMonth:      config.TaxMonth,
		CoverageFrom:  config.CoverageFrom,
		CoverageTo:    config.CoverageTo,
	}); err != nil {
		return Result{}, err
	}
	if !result.Ready {
		return result, errors.New("preview validation failed; inspect validation-errors.csv")
	}
	return result, nil
}

type manifest struct {
	MasterFile    string    `json:"masterFile"`
	PayrollFile   string    `json:"payrollFile"`
	MasterSHA256  string    `json:"masterSha256"`
	PayrollSHA256 string    `json:"payrollSha256"`
	TaxYear       int       `json:"taxYear"`
	TaxMonth      int       `json:"taxMonth"`
	CoverageFrom  time.Time `json:"coverageFrom"`
	CoverageTo    time.Time `json:"coverageTo"`
}

func validateConfig(config Config) error {
	if config.MaxRows <= 0 || config.TaxYear < 2000 || config.TaxMonth < 1 || config.TaxMonth > 12 {
		return errors.New("invalid preview configuration")
	}
	if config.CoverageFrom.IsZero() || config.CoverageTo.IsZero() || config.CoverageFrom.After(config.CoverageTo) {
		return errors.New("invalid payroll coverage range")
	}
	return nil
}

type previewInput struct {
	Data           []byte
	Filename       string
	Extension      string
	Operation      filedomain.Operation
	Configuration  []byte
	PayrollContext *filedomain.PayrollImportContext
}

func createPreviewBatch(ctx context.Context, repository *memoryRepository, storage *objectstorage.Filesystem, input previewInput) (filedomain.ImportBatch, error) {
	templateID, err := filedomain.NewTemplateID()
	if err != nil {
		return filedomain.ImportBatch{}, err
	}
	repository.templates[templateID] = filedomain.ImportTemplate{
		ID:            templateID,
		TenantID:      &previewTenantID,
		CompanyID:     &previewCompanyID,
		TemplateType:  string(input.Operation),
		Version:       "preview-v1",
		FileFormat:    input.Extension,
		Status:        "active",
		Configuration: input.Configuration,
	}

	fileID, err := filedomain.NewFileObjectID()
	if err != nil {
		return filedomain.ImportBatch{}, err
	}
	batchID, err := filedomain.NewBatchID()
	if err != nil {
		return filedomain.ImportBatch{}, err
	}
	fileKey := fmt.Sprintf("preview/%s/input.%s", batchID.String(), input.Extension)
	mimeType := mimeForExtension(input.Extension)
	info, err := storage.Put(ctx, fileKey, bytes.NewReader(input.Data), int64(len(input.Data)), mimeType)
	if err != nil {
		return filedomain.ImportBatch{}, err
	}
	now := time.Now().UTC()
	file := filedomain.FileObject{
		ID:                fileID,
		TenantID:          previewTenantID,
		CompanyID:         previewCompanyID,
		StorageProvider:   "filesystem",
		ObjectKey:         fileKey,
		OriginalFilename:  input.Filename,
		DetectedExtension: input.Extension,
		DetectedMIMEType:  mimeType,
		SizeBytes:         info.SizeBytes,
		SHA256:            info.SHA256,
		EncryptionMode:    "filesystem-private",
		Status:            filedomain.FileObjectAvailable,
		CreatedBy:         previewUserID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	batch, err := filedomain.NewImportBatch(batchID, previewTenantID, previewCompanyID, input.Operation, fileID, previewUserID, now)
	if err != nil {
		return filedomain.ImportBatch{}, err
	}
	batch.TemplateID = &templateID
	batch.Status = filedomain.BatchUploaded
	batch.PayrollContext = input.PayrollContext
	if _, err := repository.CreateBatch(ctx, file, batch); err != nil {
		return filedomain.ImportBatch{}, err
	}
	return batch, nil
}

func mimeForExtension(extension string) string {
	switch extension {
	case "xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case "xlsm":
		return "application/vnd.ms-excel.sheet.macroEnabled.12"
	default:
		return "application/octet-stream"
	}
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}
