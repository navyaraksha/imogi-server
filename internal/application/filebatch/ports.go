package filebatch

import (
	"context"
	"time"

	domain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	jobdomain "github.com/navyaraksha/imogi/internal/domain/job"
)

type Repository interface {
	CreateBatch(context.Context, domain.FileObject, domain.ImportBatch) (domain.ImportBatch, error)
	GetBatch(context.Context, domain.BatchID) (domain.ImportBatch, error)
	GetFileObject(context.Context, domain.FileObjectID) (domain.FileObject, error)
	ListTemplates(context.Context) ([]domain.ImportTemplate, error)
	CreateArtifactFile(context.Context, domain.FileObject, domain.BatchID, domain.ArtifactID, domain.ArtifactRole) error
	CompleteUploadWithValidationJob(context.Context, UploadCompletion) (domain.ImportBatch, jobdomain.Job, error)
	RequestCommitWithJob(context.Context, CommitRequest) (domain.ImportBatch, jobdomain.Job, error)
	RequestCancellation(context.Context, domain.BatchID) (domain.ImportBatch, error)
	UpdateBatchStatus(context.Context, BatchStatusUpdate) (domain.ImportBatch, error)
	CreateArtifact(context.Context, domain.ArtifactID, domain.BatchID, domain.FileObjectID, domain.ArtifactRole) error
	ListArtifacts(context.Context, domain.BatchID) ([]Artifact, error)
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
