package filebatch

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/navyaraksha/imogi/internal/domain/identity"
	"github.com/navyaraksha/imogi/internal/domain/organization"
)

type Operation string

const (
	OperationEmployeeMaster    Operation = "employee_master"
	OperationEmploymentHistory Operation = "employment_history"
	OperationAssignmentHistory Operation = "assignment_history"
	OperationTaxProfileHistory Operation = "tax_profile_history"
	OperationPayrollLedger     Operation = "payroll_ledger"
)

func (operation Operation) Valid() bool {
	switch operation {
	case OperationEmployeeMaster, OperationEmploymentHistory, OperationAssignmentHistory, OperationTaxProfileHistory, OperationPayrollLedger:
		return true
	default:
		return false
	}
}

type BatchStatus string

const (
	BatchUploading        BatchStatus = "uploading"
	BatchUploaded         BatchStatus = "uploaded"
	BatchValidating       BatchStatus = "validating"
	BatchValidated        BatchStatus = "validated"
	BatchValidationFailed BatchStatus = "validation_failed"
	BatchCommitRequested  BatchStatus = "commit_requested"
	BatchCommitting       BatchStatus = "committing"
	BatchCompleted        BatchStatus = "completed"
	BatchCommitFailed     BatchStatus = "commit_failed"
	BatchCancelRequested  BatchStatus = "cancel_requested"
	BatchCanceled         BatchStatus = "canceled"
)

type FileObjectStatus string

const (
	FileObjectPending   FileObjectStatus = "pending"
	FileObjectAvailable FileObjectStatus = "available"
	FileObjectExpired   FileObjectStatus = "expired"
	FileObjectDeleted   FileObjectStatus = "deleted"
)

type ArtifactRole string

const (
	ArtifactInput             ArtifactRole = "input"
	ArtifactValidationSummary ArtifactRole = "validation_summary"
	ArtifactValidationErrors  ArtifactRole = "validation_errors"
	ArtifactNormalized        ArtifactRole = "normalized"
	ArtifactImportReceipt     ArtifactRole = "import_receipt"
	ArtifactOutputXML         ArtifactRole = "output_xml"
	ArtifactErrorReport       ArtifactRole = "error_report"
)

type ImportTemplate struct {
	ID           TemplateID
	TemplateType string
	Version      string
	FileFormat   string
	Status       string
}

type FileObject struct {
	ID                FileObjectID
	TenantID          organization.TenantID
	CompanyID         organization.CompanyID
	StorageProvider   string
	ObjectKey         string
	OriginalFilename  string
	DetectedExtension string
	DetectedMIMEType  string
	SizeBytes         int64
	SHA256            []byte
	EncryptionMode    string
	Status            FileObjectStatus
	ExpiresAt         *time.Time
	LegalHold         bool
	CreatedBy         identity.UserID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ImportBatch struct {
	ID                   BatchID
	TenantID             organization.TenantID
	CompanyID            organization.CompanyID
	Operation            Operation
	InputFileID          FileObjectID
	TemplateID           *TemplateID
	CreatedBy            identity.UserID
	Status               BatchStatus
	TotalRows            int
	ValidRows            int
	InvalidRows          int
	WarningRows          int
	CommittedRows        int
	RejectedRows         int
	ValidationStartedAt  *time.Time
	ValidationFinishedAt *time.Time
	CommitStartedAt      *time.Time
	CommitFinishedAt     *time.Time
	ExpiresAt            *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func NewImportBatch(
	id BatchID,
	tenantID organization.TenantID,
	companyID organization.CompanyID,
	operation Operation,
	inputFileID FileObjectID,
	createdBy identity.UserID,
	now time.Time,
) (ImportBatch, error) {
	if id.UUID() == uuid.Nil || tenantID.UUID() == uuid.Nil || companyID.UUID() == uuid.Nil || inputFileID.UUID() == uuid.Nil || createdBy.UUID() == uuid.Nil {
		return ImportBatch{}, fmt.Errorf("%w: identifiers are required", ErrInvalidBatch)
	}
	if !operation.Valid() {
		return ImportBatch{}, fmt.Errorf("%w: %s", ErrUnsupportedOperation, operation)
	}
	now = now.UTC()
	return ImportBatch{ID: id, TenantID: tenantID, CompanyID: companyID, Operation: operation, InputFileID: inputFileID, CreatedBy: createdBy, Status: BatchUploading, CreatedAt: now, UpdatedAt: now}, nil
}

func (batch ImportBatch) CanCommit() bool {
	return batch.Status == BatchValidated && batch.InvalidRows == 0
}

func (batch ImportBatch) RequestCommit(now time.Time) (ImportBatch, error) {
	if !batch.CanCommit() {
		return ImportBatch{}, ErrBatchValidationRequired
	}
	batch.Status = BatchCommitRequested
	batch.UpdatedAt = now.UTC()
	return batch, nil
}

func (batch ImportBatch) RequestCancel(now time.Time) (ImportBatch, error) {
	switch batch.Status {
	case BatchUploading, BatchUploaded, BatchValidating, BatchValidated, BatchValidationFailed, BatchCommitRequested, BatchCommitFailed:
		batch.Status = BatchCancelRequested
		batch.UpdatedAt = now.UTC()
		return batch, nil
	case BatchCompleted, BatchCanceled:
		return ImportBatch{}, ErrBatchInvalidState
	default:
		return ImportBatch{}, ErrBatchInvalidState
	}
}

func normalizeExtension(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), ".")
}
