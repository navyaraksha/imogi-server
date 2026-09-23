package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	appfilebatch "github.com/navyaraksha/imogi/internal/application/filebatch"
	domain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
	jobdomain "github.com/navyaraksha/imogi/internal/domain/job"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	"github.com/navyaraksha/imogi/internal/infrastructure/postgres/sqlc"
)

type FileBatchRepository struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

var _ appfilebatch.Repository = (*FileBatchRepository)(nil)

func NewFileBatchRepository(pool *pgxpool.Pool) (*FileBatchRepository, error) {
	if pool == nil {
		return nil, errors.New("postgres pool is required")
	}
	return &FileBatchRepository{pool: pool, queries: sqlc.New(pool)}, nil
}

func (repository *FileBatchRepository) CreateBatch(ctx context.Context, file domain.FileObject, batch domain.ImportBatch) (domain.ImportBatch, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return domain.ImportBatch{}, fmt.Errorf("begin file batch transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := repository.queries.WithTx(tx)
	if _, err := queries.CreateFileObject(ctx, fileObjectParams(file)); err != nil {
		return domain.ImportBatch{}, err
	}
	row, err := queries.CreateImportBatch(ctx, importBatchParams(batch))
	if err != nil {
		return domain.ImportBatch{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ImportBatch{}, fmt.Errorf("commit file batch transaction: %w", err)
	}
	return mapImportBatch(row), nil
}

func (repository *FileBatchRepository) GetBatch(ctx context.Context, id domain.BatchID) (domain.ImportBatch, error) {
	row, err := repository.queries.GetImportBatch(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportBatch{}, domain.ErrBatchNotFound
	}
	if err != nil {
		return domain.ImportBatch{}, err
	}
	return mapImportBatch(row), nil
}

func (repository *FileBatchRepository) GetFileObject(ctx context.Context, id domain.FileObjectID) (domain.FileObject, error) {
	row, err := repository.queries.GetFileObject(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FileObject{}, domain.ErrFileObjectNotFound
	}
	if err != nil {
		return domain.FileObject{}, err
	}
	return mapFileObject(row), nil
}

func (repository *FileBatchRepository) ListTemplates(ctx context.Context) ([]domain.ImportTemplate, error) {
	rows, err := repository.queries.ListImportTemplates(ctx)
	if err != nil {
		return nil, err
	}
	templates := make([]domain.ImportTemplate, 0, len(rows))
	for _, row := range rows {
		templates = append(templates, domain.ImportTemplate{
			ID:           domain.TemplateID(row.ID),
			TemplateType: row.TemplateType,
			Version:      row.Version,
			FileFormat:   row.FileFormat,
			Status:       row.Status,
		})
	}
	return templates, nil
}

func (repository *FileBatchRepository) CreateArtifactFile(ctx context.Context, file domain.FileObject, batchID domain.BatchID, artifactID domain.ArtifactID, role domain.ArtifactRole) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := repository.queries.WithTx(tx)
	if _, err := queries.CreateFileObject(ctx, fileObjectParams(file)); err != nil {
		return err
	}
	if _, err := queries.CreateBatchArtifact(ctx, sqlc.CreateBatchArtifactParams{ID: artifactID.UUID(), BatchID: batchID.UUID(), FileObjectID: file.ID.UUID(), Role: string(role)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repository *FileBatchRepository) CompleteUploadWithValidationJob(ctx context.Context, completion appfilebatch.UploadCompletion) (domain.ImportBatch, jobdomain.Job, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, fmt.Errorf("begin upload completion transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := repository.queries.WithTx(tx)
	if _, err := queries.MarkFileObjectAvailable(ctx, sqlc.MarkFileObjectAvailableParams{
		ID:        completion.FileObjectID.UUID(),
		SizeBytes: completion.SizeBytes,
		Sha256:    completion.SHA256,
	}); err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	if _, err := queries.UpdateImportBatchStatus(ctx, sqlc.UpdateImportBatchStatusParams{
		ID:     completion.BatchID.UUID(),
		Status: string(domain.BatchUploaded),
	}); err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	jobRow, err := queries.CreateBackgroundJob(ctx, backgroundJobParams(completion.ValidationJob))
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	if _, err := queries.CreateImportBatchStep(ctx, sqlc.CreateImportBatchStepParams{
		ID:       completion.StepID.UUID(),
		BatchID:  completion.BatchID.UUID(),
		StepType: "validation",
		JobID:    completion.ValidationJob.ID.UUID(),
	}); err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	batchRow, err := queries.GetImportBatch(ctx, completion.BatchID.UUID())
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, fmt.Errorf("commit upload completion transaction: %w", err)
	}
	return mapImportBatch(batchRow), mapBackgroundJob(jobRow), nil
}

func (repository *FileBatchRepository) RequestCommitWithJob(ctx context.Context, request appfilebatch.CommitRequest) (domain.ImportBatch, jobdomain.Job, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, fmt.Errorf("begin commit request transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := repository.queries.WithTx(tx)
	batchRow, err := queries.RequestImportBatchCommit(ctx, request.BatchID.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportBatch{}, jobdomain.Job{}, domain.ErrBatchValidationRequired
	}
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	jobRow, err := queries.CreateBackgroundJob(ctx, backgroundJobParams(request.Job))
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	if _, err := queries.CreateImportBatchStep(ctx, sqlc.CreateImportBatchStepParams{ID: request.StepID.UUID(), BatchID: request.BatchID.UUID(), StepType: "commit", JobID: request.Job.ID.UUID()}); err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, fmt.Errorf("commit commit request transaction: %w", err)
	}
	return mapImportBatch(batchRow), mapBackgroundJob(jobRow), nil
}

func (repository *FileBatchRepository) RequestCancellation(ctx context.Context, id domain.BatchID) (domain.ImportBatch, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return domain.ImportBatch{}, fmt.Errorf("begin batch cancellation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := repository.queries.WithTx(tx)
	row, err := queries.UpdateImportBatchStatus(ctx, sqlc.UpdateImportBatchStatusParams{ID: id.UUID(), Status: string(domain.BatchCancelRequested)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportBatch{}, domain.ErrBatchNotFound
	}
	if err != nil {
		return domain.ImportBatch{}, err
	}
	if err := queries.CancelBackgroundJobsForBatch(ctx, id.UUID()); err != nil {
		return domain.ImportBatch{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ImportBatch{}, fmt.Errorf("commit batch cancellation transaction: %w", err)
	}
	return mapImportBatch(row), nil
}

func (repository *FileBatchRepository) UpdateBatchStatus(ctx context.Context, update appfilebatch.BatchStatusUpdate) (domain.ImportBatch, error) {
	row, err := repository.queries.UpdateImportBatchStatus(ctx, sqlc.UpdateImportBatchStatusParams{
		ID:                   update.BatchID.UUID(),
		Status:               string(update.Status),
		ValidationStartedAt:  nullablePGTimestamp(update.ValidationStartedAt),
		ValidationFinishedAt: nullablePGTimestamp(update.ValidationFinishedAt),
		CommitStartedAt:      nullablePGTimestamp(update.CommitStartedAt),
		CommitFinishedAt:     nullablePGTimestamp(update.CommitFinishedAt),
		TotalRows:            int32Ptr(update.TotalRows),
		ValidRows:            int32Ptr(update.ValidRows),
		InvalidRows:          int32Ptr(update.InvalidRows),
		WarningRows:          int32Ptr(update.WarningRows),
		CommittedRows:        int32Ptr(update.CommittedRows),
		RejectedRows:         int32Ptr(update.RejectedRows),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportBatch{}, domain.ErrBatchNotFound
	}
	if err != nil {
		return domain.ImportBatch{}, err
	}
	return mapImportBatch(row), nil
}

func (repository *FileBatchRepository) CreateArtifact(ctx context.Context, id domain.ArtifactID, batchID domain.BatchID, fileID domain.FileObjectID, role domain.ArtifactRole) error {
	_, err := repository.queries.CreateBatchArtifact(ctx, sqlc.CreateBatchArtifactParams{
		ID:           id.UUID(),
		BatchID:      batchID.UUID(),
		FileObjectID: fileID.UUID(),
		Role:         string(role),
	})
	return err
}

func (repository *FileBatchRepository) ListArtifacts(ctx context.Context, batchID domain.BatchID) ([]appfilebatch.Artifact, error) {
	rows, err := repository.queries.ListBatchArtifacts(ctx, batchID.UUID())
	if err != nil {
		return nil, err
	}
	artifacts := make([]appfilebatch.Artifact, 0, len(rows))
	for _, row := range rows {
		file, err := repository.GetFileObject(ctx, domain.FileObjectID(row.FileObjectID))
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, appfilebatch.Artifact{
			ID:         domain.ArtifactID(row.ID),
			BatchID:    domain.BatchID(row.BatchID),
			FileObject: file,
			Role:       domain.ArtifactRole(row.Role),
			CreatedAt:  timestamp(row.CreatedAt),
		})
	}
	return artifacts, nil
}

func fileObjectParams(file domain.FileObject) sqlc.CreateFileObjectParams {
	var createdBy *uuid.UUID
	if file.CreatedBy.UUID() != uuid.Nil {
		createdBy = uuidPtr(file.CreatedBy.UUID())
	}
	return sqlc.CreateFileObjectParams{
		ID:                file.ID.UUID(),
		TenantID:          uuidPtr(file.TenantID.UUID()),
		CompanyID:         uuidPtr(file.CompanyID.UUID()),
		StorageProvider:   file.StorageProvider,
		ObjectKey:         file.ObjectKey,
		OriginalFilename:  file.OriginalFilename,
		DetectedExtension: file.DetectedExtension,
		DetectedMimeType:  file.DetectedMIMEType,
		SizeBytes:         file.SizeBytes,
		Sha256:            file.SHA256,
		EncryptionMode:    file.EncryptionMode,
		Status:            string(file.Status),
		ExpiresAt:         nullablePGTimestamp(file.ExpiresAt),
		CreatedBy:         createdBy,
	}
}

func importBatchParams(batch domain.ImportBatch) sqlc.CreateImportBatchParams {
	var templateID *uuid.UUID
	if batch.TemplateID != nil {
		value := batch.TemplateID.UUID()
		templateID = &value
	}
	return sqlc.CreateImportBatchParams{
		ID:          batch.ID.UUID(),
		TenantID:    batch.TenantID.UUID(),
		CompanyID:   batch.CompanyID.UUID(),
		Operation:   string(batch.Operation),
		InputFileID: batch.InputFileID.UUID(),
		TemplateID:  templateID,
		CreatedBy:   batch.CreatedBy.UUID(),
		ExpiresAt:   nullablePGTimestamp(batch.ExpiresAt),
	}
}

func uuidPtr(value uuid.UUID) *uuid.UUID {
	return &value
}

func nullablePGTimestamp(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return toPGTimestamp(*value)
}

func int32Ptr(value *int) *int32 {
	if value == nil {
		return nil
	}
	converted := int32(*value)
	return &converted
}

func backgroundJobParams(entity jobdomain.Job) sqlc.CreateBackgroundJobParams {
	return sqlc.CreateBackgroundJobParams{
		ID:             entity.ID.UUID(),
		TenantID:       optionalTenantUUID(entity.TenantID),
		CompanyID:      optionalCompanyUUID(entity.CompanyID),
		JobType:        entity.JobType,
		QueueName:      entity.QueueName,
		Payload:        []byte(entity.Payload),
		IdempotencyKey: entity.IdempotencyKey,
		AvailableAt:    toPGTimestamp(entity.AvailableAt),
		MaxAttempts:    int32(entity.MaxAttempts),
	}
}

func mapFileObject(row sqlc.FileFileObject) domain.FileObject {
	var createdBy identitydomain.UserID
	if row.CreatedBy != nil {
		createdBy = identitydomain.UserID(*row.CreatedBy)
	}
	return domain.FileObject{
		ID:                domain.FileObjectID(row.ID),
		TenantID:          organization.TenantID(*row.TenantID),
		CompanyID:         organization.CompanyID(*row.CompanyID),
		StorageProvider:   row.StorageProvider,
		ObjectKey:         row.ObjectKey,
		OriginalFilename:  row.OriginalFilename,
		DetectedExtension: row.DetectedExtension,
		DetectedMIMEType:  row.DetectedMimeType,
		SizeBytes:         row.SizeBytes,
		SHA256:            row.Sha256,
		EncryptionMode:    row.EncryptionMode,
		Status:            domain.FileObjectStatus(row.Status),
		ExpiresAt:         nullableTimestamp(row.ExpiresAt),
		LegalHold:         row.LegalHold,
		CreatedBy:         createdBy,
		CreatedAt:         timestamp(row.CreatedAt),
		UpdatedAt:         timestamp(row.UpdatedAt),
	}
}

func mapImportBatch(row sqlc.FileImportBatch) domain.ImportBatch {
	var templateID *domain.TemplateID
	if row.TemplateID != nil {
		value := domain.TemplateID(*row.TemplateID)
		templateID = &value
	}
	return domain.ImportBatch{
		ID:                   domain.BatchID(row.ID),
		TenantID:             organization.TenantID(row.TenantID),
		CompanyID:            organization.CompanyID(row.CompanyID),
		Operation:            domain.Operation(row.Operation),
		InputFileID:          domain.FileObjectID(row.InputFileID),
		TemplateID:           templateID,
		CreatedBy:            identitydomain.UserID(row.CreatedBy),
		Status:               domain.BatchStatus(row.Status),
		TotalRows:            int(row.TotalRows),
		ValidRows:            int(row.ValidRows),
		InvalidRows:          int(row.InvalidRows),
		WarningRows:          int(row.WarningRows),
		CommittedRows:        int(row.CommittedRows),
		RejectedRows:         int(row.RejectedRows),
		ValidationStartedAt:  nullableTimestamp(row.ValidationStartedAt),
		ValidationFinishedAt: nullableTimestamp(row.ValidationFinishedAt),
		CommitStartedAt:      nullableTimestamp(row.CommitStartedAt),
		CommitFinishedAt:     nullableTimestamp(row.CommitFinishedAt),
		ExpiresAt:            nullableTimestamp(row.ExpiresAt),
		CreatedAt:            timestamp(row.CreatedAt),
		UpdatedAt:            timestamp(row.UpdatedAt),
	}
}
