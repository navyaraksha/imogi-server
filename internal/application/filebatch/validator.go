package filebatch

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	domain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
	jobdomain "github.com/navyaraksha/imogi/internal/domain/job"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	platformstorage "github.com/navyaraksha/imogi/internal/platform/objectstorage"
	"github.com/navyaraksha/imogi/internal/platform/tabular"
)

type RowReader = tabular.RowReader
type Parser = tabular.Parser

type Validator struct {
	repository Repository
	storage    platformstorage.Storage
	parser     Parser
	clock      clock.Clock
	maxRows    int
}

func NewValidator(repository Repository, storage platformstorage.Storage, parser Parser, systemClock clock.Clock, maxRows int) (*Validator, error) {
	if repository == nil || storage == nil || parser == nil || systemClock == nil || maxRows <= 0 {
		return nil, errors.New("validator dependencies are required")
	}
	return &Validator{repository: repository, storage: storage, parser: parser, clock: systemClock, maxRows: maxRows}, nil
}

func (validator *Validator) Handle(ctx context.Context, job jobdomain.Job) error {
	var payload struct {
		BatchID string `json:"batchId"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil || payload.BatchID == "" {
		return fmt.Errorf("invalid validation payload: %w", err)
	}
	batchID, err := domain.ParseBatchID(payload.BatchID)
	if err != nil {
		return err
	}
	return validator.Validate(ctx, batchID)
}

func (validator *Validator) Validate(ctx context.Context, batchID domain.BatchID) error {
	startedAt := validator.clock.Now().UTC()
	batch, err := validator.repository.GetBatch(ctx, batchID)
	if err != nil {
		return err
	}
	if _, err := validator.repository.UpdateBatchStatus(ctx, BatchStatusUpdate{BatchID: batchID, Status: domain.BatchValidating, ValidationStartedAt: &startedAt}); err != nil {
		return err
	}
	file, err := validator.repository.GetFileObject(ctx, batch.InputFileID)
	if err != nil {
		return err
	}
	input, _, err := validator.storage.Open(ctx, file.ObjectKey)
	if err != nil {
		return fmt.Errorf("open input object: %w", err)
	}
	defer input.Close()
	rows, err := validator.parser.Open(input, file.DetectedExtension)
	if err != nil {
		return validator.fail(ctx, batchID, startedAt, err)
	}
	defer rows.Close()

	header, err := nextColumns(rows)
	if err != nil {
		return validator.fail(ctx, batchID, startedAt, err)
	}
	if err := validateHeader(batch.Operation, header); err != nil {
		return validator.failWithCounts(ctx, batchID, startedAt, 0, 0, 1, 0, err)
	}

	result := validationResult{Operation: string(batch.Operation), Headers: header, Errors: make([]validationError, 0)}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		result.TotalRows++
		if result.TotalRows > validator.maxRows {
			result.Errors = append(result.Errors, validationError{Row: result.TotalRows, Code: "ROW_LIMIT_EXCEEDED", Message: "maximum row limit exceeded"})
			break
		}
		values, err := rows.Columns()
		if err != nil {
			return validator.fail(ctx, batchID, startedAt, err)
		}
		if err := validateEmployeeRow(batch.Operation, header, values); err != nil {
			result.InvalidRows++
			result.Errors = append(result.Errors, validationError{Row: result.TotalRows, Code: "INVALID_ROW", Message: err.Error()})
		} else {
			result.ValidRows++
		}
	}
	if err := rows.Err(); err != nil {
		return validator.fail(ctx, batchID, startedAt, err)
	}
	result.FinishedAt = validator.clock.Now().UTC()
	if err := validator.writeArtifacts(ctx, batch, result); err != nil {
		return validator.fail(ctx, batchID, startedAt, err)
	}
	status := domain.BatchValidated
	if result.InvalidRows > 0 {
		status = domain.BatchValidationFailed
	}
	_, err = validator.repository.UpdateBatchStatus(ctx, BatchStatusUpdate{BatchID: batchID, Status: status, ValidationFinishedAt: &result.FinishedAt, TotalRows: intPtr(result.TotalRows), ValidRows: intPtr(result.ValidRows), InvalidRows: intPtr(result.InvalidRows), WarningRows: intPtr(result.WarningRows)})
	return err
}

type validationResult struct {
	Operation   string            `json:"operation"`
	Headers     []string          `json:"headers"`
	TotalRows   int               `json:"totalRows"`
	ValidRows   int               `json:"validRows"`
	InvalidRows int               `json:"invalidRows"`
	WarningRows int               `json:"warningRows"`
	Errors      []validationError `json:"errors"`
	FinishedAt  time.Time         `json:"finishedAt"`
}

type validationError struct {
	Row     int    `json:"row"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (validator *Validator) writeArtifacts(ctx context.Context, batch domain.ImportBatch, result validationResult) error {
	summary, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := validator.writeArtifact(ctx, batch, domain.ArtifactValidationSummary, "validation-summary.json", "json", "application/json", summary); err != nil {
		return err
	}
	if len(result.Errors) == 0 {
		return nil
	}
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	_ = writer.Write([]string{"row", "code", "message"})
	for _, item := range result.Errors {
		_ = writer.Write([]string{strconv.Itoa(item.Row), item.Code, item.Message})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	return validator.writeArtifact(ctx, batch, domain.ArtifactValidationErrors, "validation-errors.csv", "csv", "text/csv", output.Bytes())
}

func (validator *Validator) writeArtifact(ctx context.Context, batch domain.ImportBatch, role domain.ArtifactRole, filename, extension, mimeType string, data []byte) error {
	fileID, err := domain.NewFileObjectID()
	if err != nil {
		return err
	}
	artifactID, err := domain.NewArtifactID()
	if err != nil {
		return err
	}
	key := fmt.Sprintf("tenants/%s/companies/%s/batches/%s/%s", batch.TenantID.String(), batch.CompanyID.String(), batch.ID.String(), filename)
	info, err := validator.storage.Put(ctx, key, bytes.NewReader(data), int64(len(data)), mimeType)
	if err != nil {
		return err
	}
	provider := validator.storage.Provider()
	encryptionMode := "filesystem-private"
	if provider == "s3" {
		encryptionMode = "sse-s3"
	}
	file := domain.FileObject{ID: fileID, TenantID: batch.TenantID, CompanyID: batch.CompanyID, StorageProvider: provider, ObjectKey: key, OriginalFilename: filename, DetectedExtension: extension, DetectedMIMEType: mimeType, SizeBytes: info.SizeBytes, SHA256: info.SHA256, EncryptionMode: encryptionMode, Status: domain.FileObjectAvailable, CreatedBy: identitydomain.UserID{}, CreatedAt: validator.clock.Now().UTC(), UpdatedAt: validator.clock.Now().UTC()}
	if err := validator.repository.CreateArtifactFile(ctx, file, batch.ID, artifactID, role); err != nil {
		return err
	}
	return nil
}

func (validator *Validator) fail(ctx context.Context, batchID domain.BatchID, startedAt time.Time, err error) error {
	return validator.failWithCounts(ctx, batchID, startedAt, 0, 0, 0, 0, err)
}

func (validator *Validator) failWithCounts(ctx context.Context, batchID domain.BatchID, startedAt time.Time, total, valid, invalid, warnings int, cause error) error {
	finished := validator.clock.Now().UTC()
	_, updateErr := validator.repository.UpdateBatchStatus(ctx, BatchStatusUpdate{BatchID: batchID, Status: domain.BatchValidationFailed, ValidationStartedAt: &startedAt, ValidationFinishedAt: &finished, TotalRows: &total, ValidRows: &valid, InvalidRows: &invalid, WarningRows: &warnings})
	if updateErr != nil {
		return fmt.Errorf("%v; update failed batch: %w", cause, updateErr)
	}
	return cause
}

func nextColumns(rows RowReader) ([]string, error) {
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("input file has no header row")
	}
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	for index := range columns {
		columns[index] = normalizeColumn(columns[index])
	}
	return columns, nil
}

func validateHeader(operation domain.Operation, header []string) error {
	if len(header) == 0 {
		return errors.New("input file has an empty header row")
	}
	if operation == domain.OperationEmployeeMaster {
		for _, required := range []string{"nik", "full_name", "employee_number"} {
			if !containsColumn(header, required) {
				return fmt.Errorf("missing required column %q", required)
			}
		}
	}
	return nil
}

func validateEmployeeRow(operation domain.Operation, header, values []string) error {
	if operation != domain.OperationEmployeeMaster {
		return nil
	}
	row := make(map[string]string, len(header))
	for index, name := range header {
		if index < len(values) {
			row[name] = strings.TrimSpace(values[index])
		}
	}
	if len([]rune(row["nik"])) != 16 {
		return errors.New("nik must contain exactly 16 digits")
	}
	for _, character := range row["nik"] {
		if character < '0' || character > '9' {
			return errors.New("nik must contain only digits")
		}
	}
	if row["full_name"] == "" {
		return errors.New("full_name is required")
	}
	return nil
}

func normalizeColumn(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "_")
	switch value {
	case "nama", "name":
		return "full_name"
	case "nomor_ktp", "no_ktp", "ktp":
		return "nik"
	default:
		return value
	}
}

func containsColumn(columns []string, expected string) bool {
	for _, column := range columns {
		if column == expected {
			return true
		}
	}
	return false
}

func intPtr(value int) *int { return &value }
