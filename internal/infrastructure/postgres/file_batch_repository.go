package postgres

import (
	"context"
	"encoding/json"
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
	if batch.PayrollContext != nil {
		if _, err := queries.CreateImportBatchPayrollContext(ctx, payrollContextParams(batch.ID, batch.TenantID, batch.CompanyID, *batch.PayrollContext)); err != nil {
			return domain.ImportBatch{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ImportBatch{}, fmt.Errorf("commit file batch transaction: %w", err)
	}
	return mapImportBatch(row), nil
}

func (repository *FileBatchRepository) SavePayrollContext(ctx context.Context, id domain.BatchID, value domain.PayrollImportContext) error {
	batch, err := repository.GetBatch(ctx, id)
	if err != nil {
		return err
	}
	_, err = repository.queries.CreateImportBatchPayrollContext(ctx, payrollContextParams(batch.ID, batch.TenantID, batch.CompanyID, value))
	return err
}

func (repository *FileBatchRepository) GetBatch(ctx context.Context, id domain.BatchID) (domain.ImportBatch, error) {
	row, err := repository.queries.GetImportBatch(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportBatch{}, domain.ErrBatchNotFound
	}
	if err != nil {
		return domain.ImportBatch{}, err
	}
	entity := mapImportBatch(row)
	if entity.Operation == domain.OperationPayrollLedger {
		contextRow, contextErr := repository.queries.GetImportBatchPayrollContext(ctx, id.UUID())
		if contextErr != nil {
			return domain.ImportBatch{}, contextErr
		}
		entity.PayrollContext = mapPayrollImportContext(contextRow)
	}
	return entity, nil
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

func (repository *FileBatchRepository) ListTemplates(ctx context.Context, tenantID uuid.UUID) ([]domain.ImportTemplate, error) {
	rows, err := repository.queries.ListImportTemplates(ctx, uuidPtr(tenantID))
	if err != nil {
		return nil, err
	}
	templates := make([]domain.ImportTemplate, 0, len(rows))
	for _, row := range rows {
		var scopedTenantID *organization.TenantID
		if row.TenantID != nil {
			value := organization.TenantID(*row.TenantID)
			scopedTenantID = &value
		}
		var scopedCompanyID *organization.CompanyID
		if row.CompanyID != nil {
			value := organization.CompanyID(*row.CompanyID)
			scopedCompanyID = &value
		}
		templates = append(templates, domain.ImportTemplate{
			ID: domain.TemplateID(row.ID), TenantID: scopedTenantID, CompanyID: scopedCompanyID,
			TemplateType:  row.TemplateType,
			Version:       row.Version,
			FileFormat:    row.FileFormat,
			Status:        row.Status,
			Configuration: json.RawMessage(`{}`),
		})
	}
	return templates, nil
}

func (repository *FileBatchRepository) GetTemplate(ctx context.Context, id domain.TemplateID) (domain.ImportTemplate, error) {
	row, err := repository.queries.GetImportTemplate(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportTemplate{}, domain.ErrTemplateNotFound
	}
	if err != nil {
		return domain.ImportTemplate{}, err
	}
	return domain.ImportTemplate{
		ID: domain.TemplateID(row.ID), TenantID: mapTenantID(row.TenantID), CompanyID: mapCompanyID(row.CompanyID), TemplateType: row.TemplateType, Version: row.Version,
		FileFormat: row.FileFormat, Status: row.Status, Configuration: append(json.RawMessage(nil), row.Configuration...),
	}, nil
}

func (repository *FileBatchRepository) CreateTemplate(ctx context.Context, template domain.ImportTemplate) (domain.ImportTemplate, error) {
	row, err := repository.queries.CreateImportTemplate(ctx, sqlc.CreateImportTemplateParams{ID: template.ID.UUID(), TenantID: scopedTenantUUID(template.TenantID), CompanyID: scopedCompanyUUID(template.CompanyID), TemplateType: template.TemplateType, Version: template.Version, FileFormat: template.FileFormat, Configuration: []byte(template.Configuration)})
	if err != nil {
		return domain.ImportTemplate{}, err
	}
	return mapImportTemplate(row), nil
}

func mapImportTemplate(row sqlc.FileImportTemplate) domain.ImportTemplate {
	return domain.ImportTemplate{ID: domain.TemplateID(row.ID), TenantID: mapTenantID(row.TenantID), CompanyID: mapCompanyID(row.CompanyID), TemplateType: row.TemplateType, Version: row.Version, FileFormat: row.FileFormat, Status: row.Status, Configuration: append(json.RawMessage(nil), row.Configuration...)}
}

func scopedTenantUUID(value *organization.TenantID) *uuid.UUID {
	if value == nil {
		return nil
	}
	id := value.UUID()
	return &id
}

func scopedCompanyUUID(value *organization.CompanyID) *uuid.UUID {
	if value == nil {
		return nil
	}
	id := value.UUID()
	return &id
}

func (repository *FileBatchRepository) RetireTemplate(ctx context.Context, id domain.TemplateID) error {
	_, err := repository.queries.RetireImportTemplate(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrTemplateNotApplicable
	}
	return err
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

func (repository *FileBatchRepository) ListValidationSheets(ctx context.Context, batchID domain.BatchID) ([]appfilebatch.ValidationSheet, error) {
	rows, err := repository.queries.ListImportValidationSheets(ctx, batchID.UUID())
	if err != nil {
		return nil, err
	}
	result := make([]appfilebatch.ValidationSheet, 0, len(rows))
	for _, row := range rows {
		result = append(result, appfilebatch.ValidationSheet{
			Name: row.SheetName, TotalRows: int(row.TotalRows), ValidRows: int(row.ValidRows),
			InvalidRows: int(row.InvalidRows), BlockingRows: int(row.BlockingRows),
		})
	}
	return result, nil
}

func (repository *FileBatchRepository) ListValidationIssues(ctx context.Context, batchID domain.BatchID) ([]appfilebatch.ValidationIssueView, error) {
	rows, err := repository.queries.ListImportValidationIssues(ctx, batchID.UUID())
	if err != nil {
		return nil, err
	}
	result := make([]appfilebatch.ValidationIssueView, 0, len(rows))
	for _, row := range rows {
		result = append(result, appfilebatch.ValidationIssueView{
			ID: domain.ImportRowIssueID(row.ID), RowID: domain.ImportRowID(row.RowID),
			SheetName: row.SheetName, RowNumber: int(row.RowNo), FieldName: row.FieldName,
			ErrorCode: row.ErrorCode, Severity: row.Severity, Description: row.Description,
			MaskedValue: row.MaskedValue, CandidateCount: int(row.CandidateCount),
		})
	}
	return result, nil
}

func (repository *FileBatchRepository) ResolveValidationRow(ctx context.Context, input appfilebatch.ResolveValidationRowInput) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin validation resolution transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := repository.queries.WithTx(tx)
	if _, err := queries.ResolveImportBatchRow(ctx, sqlc.ResolveImportBatchRowParams{
		BatchID: input.BatchID.UUID(), RowID: input.RowID.UUID(),
		EmployeeID: input.EmployeeID, EmploymentID: input.EmploymentID,
	}); err != nil {
		return err
	}
	decisionID, err := domain.NewImportRowIssueID()
	if err != nil {
		return err
	}
	if _, err := queries.CreateImportRowDecision(ctx, sqlc.CreateImportRowDecisionParams{
		ID: decisionID.UUID(), RowID: input.RowID.UUID(), EmployeeID: input.EmployeeID,
		EmploymentID: input.EmploymentID, Decision: input.Decision, Reason: input.Reason,
		DecidedBy: input.DecidedBy,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repository *FileBatchRepository) ClearValidationRows(ctx context.Context, batchID domain.BatchID) error {
	return repository.queries.ClearImportBatchValidationRows(ctx, batchID.UUID())
}

func (repository *FileBatchRepository) CreateValidationRow(ctx context.Context, row appfilebatch.ValidationRow) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin validation row transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := repository.queries.WithTx(tx)
	payload := row.NormalizedPayload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if _, err := queries.CreateImportBatchRow(ctx, sqlc.CreateImportBatchRowParams{
		ID: row.ID.UUID(), BatchID: row.BatchID.UUID(), SheetName: row.SheetName,
		RowNo: int32(row.RowNumber), SourceEmployeeNumber: row.SourceEmployeeNumber,
		SourceFullName: row.SourceFullName, MatchStatus: row.MatchStatus,
		NormalizedPayload: []byte(payload), IssueCount: int32(row.IssueCount),
		BlockingIssueCount: int32(row.BlockingIssueCount), EmployeeID: row.EmployeeID, EmploymentID: row.EmploymentID,
	}); err != nil {
		return err
	}
	for _, issue := range row.Issues {
		details := issue.Details
		if len(details) == 0 {
			details = json.RawMessage(`{}`)
		}
		if _, err := queries.CreateImportRowIssue(ctx, sqlc.CreateImportRowIssueParams{
			ID: issue.ID.UUID(), RowID: row.ID.UUID(), BatchID: row.BatchID.UUID(),
			SheetName: row.SheetName, RowNo: int32(row.RowNumber), FieldName: issue.FieldName,
			ErrorCode: issue.ErrorCode, Severity: issue.Severity, Description: issue.Description,
			MaskedValue: issue.MaskedValue, CandidateCount: int32(issue.CandidateCount),
			Details: []byte(details),
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (repository *FileBatchRepository) ListValidationRows(ctx context.Context, batchID domain.BatchID) ([]appfilebatch.ValidationRow, error) {
	rows, err := repository.queries.ListImportBatchRows(ctx, batchID.UUID())
	if err != nil {
		return nil, err
	}
	result := make([]appfilebatch.ValidationRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, appfilebatch.ValidationRow{
			ID: domain.ImportRowID(row.ID), BatchID: domain.BatchID(row.BatchID), SheetName: row.SheetName,
			RowNumber: int(row.RowNo), SourceEmployeeNumber: row.SourceEmployeeNumber, SourceFullName: row.SourceFullName,
			MatchStatus: row.MatchStatus, NormalizedPayload: append(json.RawMessage(nil), row.NormalizedPayload...),
			IssueCount: int(row.IssueCount), BlockingIssueCount: int(row.BlockingIssueCount),
			EmployeeID: row.EmployeeID, EmploymentID: row.EmploymentID,
		})
	}
	return result, nil
}

func (repository *FileBatchRepository) CreateImportRowEffect(ctx context.Context, effect appfilebatch.ImportRowEffect) error {
	_, err := repository.queries.CreateImportRowEffect(ctx, sqlc.CreateImportRowEffectParams{
		ID: effect.ID.UUID(), BatchID: effect.BatchID.UUID(), RowID: effect.RowID.UUID(),
		EntityType: effect.EntityType, EntityID: effect.EntityID, Action: effect.Action,
	})
	return err
}

func (repository *FileBatchRepository) LinkPayrollContext(ctx context.Context, batchID domain.BatchID, periodID, runID uuid.UUID) error {
	_, err := repository.queries.LinkImportBatchPayrollContext(ctx, sqlc.LinkImportBatchPayrollContextParams{BatchID: batchID.UUID(), PayrollPeriodID: &periodID, PayrollRunID: &runID})
	return err
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

func payrollContextParams(batchID domain.BatchID, tenantID organization.TenantID, companyID organization.CompanyID, value domain.PayrollImportContext) sqlc.CreateImportBatchPayrollContextParams {
	return sqlc.CreateImportBatchPayrollContextParams{
		BatchID: batchID.UUID(), TenantID: tenantID.UUID(), CompanyID: companyID.UUID(), TaxYear: int32(value.TaxYear), TaxMonth: int32(value.TaxMonth),
		CoverageFrom: toPGDate(&value.CoverageFrom), CoverageTo: toPGDate(&value.CoverageTo), PayDate: toPGDate(value.PayDate), RunType: value.RunType,
		CorrectionOfRunID: value.CorrectionOfRunID,
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

func mapPayrollImportContext(row sqlc.FileImportBatchPayrollContext) *domain.PayrollImportContext {
	return &domain.PayrollImportContext{
		TaxYear: int(row.TaxYear), TaxMonth: int(row.TaxMonth), CoverageFrom: dateValue(row.CoverageFrom), CoverageTo: dateValue(row.CoverageTo),
		PayDate: fromPGDatePtr(row.PayDate), RunType: row.RunType, PayrollPeriodID: row.PayrollPeriodID, PayrollRunID: row.PayrollRunID,
		CorrectionOfRunID: row.CorrectionOfRunID,
	}
}
