package filebatch

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	domain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	jobdomain "github.com/navyaraksha/imogi/internal/domain/job"
)

type Repository interface {
	CreateBatch(context.Context, domain.FileObject, domain.ImportBatch) (domain.ImportBatch, error)
	SavePayrollContext(context.Context, domain.BatchID, domain.PayrollImportContext) error
	GetBatch(context.Context, domain.BatchID) (domain.ImportBatch, error)
	GetFileObject(context.Context, domain.FileObjectID) (domain.FileObject, error)
	ListTemplates(context.Context, uuid.UUID) ([]domain.ImportTemplate, error)
	GetTemplate(context.Context, domain.TemplateID) (domain.ImportTemplate, error)
	CreateTemplate(context.Context, domain.ImportTemplate) (domain.ImportTemplate, error)
	RetireTemplate(context.Context, domain.TemplateID) error
	CreateArtifactFile(context.Context, domain.FileObject, domain.BatchID, domain.ArtifactID, domain.ArtifactRole) error
	CompleteUploadWithValidationJob(context.Context, UploadCompletion) (domain.ImportBatch, jobdomain.Job, error)
	RequestCommitWithJob(context.Context, CommitRequest) (domain.ImportBatch, jobdomain.Job, error)
	RequestCancellation(context.Context, domain.BatchID) (domain.ImportBatch, error)
	UpdateBatchStatus(context.Context, BatchStatusUpdate) (domain.ImportBatch, error)
	CreateArtifact(context.Context, domain.ArtifactID, domain.BatchID, domain.FileObjectID, domain.ArtifactRole) error
	ListArtifacts(context.Context, domain.BatchID) ([]Artifact, error)
	ClearValidationRows(context.Context, domain.BatchID) error
	CreateValidationRow(context.Context, ValidationRow) error
	ListValidationSheets(context.Context, domain.BatchID) ([]ValidationSheet, error)
	ListValidationIssues(context.Context, domain.BatchID) ([]ValidationIssueView, error)
	ResolveValidationRow(context.Context, ResolveValidationRowInput) error
	ListValidationRows(context.Context, domain.BatchID) ([]ValidationRow, error)
	CreateImportRowEffect(context.Context, ImportRowEffect) error
	LinkPayrollContext(context.Context, domain.BatchID, uuid.UUID, uuid.UUID) error
}

// IdentityMatcher is intentionally optional. It lets validation remain usable
// with a parser-only test double while production validation resolves source
// names and historical employee numbers against the employee ledger.
type IdentityMatcher interface {
	FindIdentityCandidates(context.Context, uuid.UUID, uuid.UUID, string, string, string) ([]IdentityCandidate, error)
}

type IdentityCandidate struct {
	EmployeeID     uuid.UUID
	EmploymentID   *uuid.UUID
	EmployeeNumber string
	FullName       string
	NIKMatch       bool
	PersonalFields map[string]string
}

type ValidationRow struct {
	ID                   domain.ImportRowID
	BatchID              domain.BatchID
	SheetName            string
	RowNumber            int
	SourceEmployeeNumber *string
	SourceFullName       *string
	MatchStatus          string
	NormalizedPayload    json.RawMessage
	IssueCount           int
	BlockingIssueCount   int
	Issues               []ValidationIssue
	EmployeeID           *uuid.UUID
	EmploymentID         *uuid.UUID
}

type ImportRowEffect struct {
	ID         domain.ImportRowID
	BatchID    domain.BatchID
	RowID      domain.ImportRowID
	EntityType string
	EntityID   uuid.UUID
	Action     string
}

type PayrollImportRow struct {
	ID           domain.ImportRowID
	SheetName    string
	RowNumber    int
	EmployeeID   *uuid.UUID
	EmploymentID *uuid.UUID
	Values       map[string]string
}

type PayrollImportWriter interface {
	CommitPayrollImport(context.Context, domain.ImportBatch, []PayrollImportRow) (PayrollImportReceipt, error)
}

type PayrollImportReceipt struct {
	PayrollPeriodID uuid.UUID  `json:"payrollPeriodId"`
	PayrollRunID    uuid.UUID  `json:"payrollRunId"`
	CoverageFrom    time.Time  `json:"coverageFrom"`
	CoverageTo      time.Time  `json:"coverageTo"`
	PayDate         *time.Time `json:"payDate,omitempty"`
	RowsCommitted   int        `json:"rowsCommitted"`
}

type ValidationIssue struct {
	ID             domain.ImportRowIssueID
	FieldName      *string
	ErrorCode      string
	Severity       string
	Description    string
	MaskedValue    *string
	CandidateCount int
	Details        json.RawMessage
}

type ValidationSheet struct {
	Name         string
	TotalRows    int
	ValidRows    int
	InvalidRows  int
	BlockingRows int
}

type ValidationIssueView struct {
	ID             domain.ImportRowIssueID
	RowID          domain.ImportRowID
	SheetName      string
	RowNumber      int
	FieldName      *string
	ErrorCode      string
	Severity       string
	Description    string
	MaskedValue    *string
	CandidateCount int
}

type ResolveValidationRowInput struct {
	BatchID      domain.BatchID
	RowID        domain.ImportRowID
	EmployeeID   *uuid.UUID
	EmploymentID *uuid.UUID
	Decision     string
	Reason       string
	DecidedBy    uuid.UUID
}

type UploadCompletion struct {
	BatchID       domain.BatchID
	FileObjectID  domain.FileObjectID
	SizeBytes     int64
	SHA256        []byte
	ValidationJob jobdomain.Job
	StepID        domain.StepID
}

type CommitRequest struct {
	BatchID domain.BatchID
	Job     jobdomain.Job
	StepID  domain.StepID
}

type BatchStatusUpdate struct {
	BatchID              domain.BatchID
	Status               domain.BatchStatus
	ValidationStartedAt  *time.Time
	ValidationFinishedAt *time.Time
	CommitStartedAt      *time.Time
	CommitFinishedAt     *time.Time
	TotalRows            *int
	ValidRows            *int
	InvalidRows          *int
	WarningRows          *int
	CommittedRows        *int
	RejectedRows         *int
}

type Artifact struct {
	ID         domain.ArtifactID
	BatchID    domain.BatchID
	FileObject domain.FileObject
	Role       domain.ArtifactRole
	CreatedAt  time.Time
}
