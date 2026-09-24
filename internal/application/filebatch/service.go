package filebatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	jobdomain "github.com/navyaraksha/imogi/internal/domain/job"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	platformstorage "github.com/navyaraksha/imogi/internal/platform/objectstorage"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

type Service struct {
	repository  Repository
	storage     platformstorage.Storage
	authorizer  security.Authorizer
	clock       clock.Clock
	presignTTL  time.Duration
	maxBytes    int64
	maxAttempts int
}

func NewService(repository Repository, storage platformstorage.Storage, authorizer security.Authorizer, systemClock clock.Clock, presignTTL time.Duration, maxBytes int64, maxAttempts int) (*Service, error) {
	if repository == nil || storage == nil || authorizer == nil || systemClock == nil || presignTTL <= 0 || maxBytes <= 0 || maxAttempts < 1 || maxAttempts > 20 {
		return nil, errors.New("file batch service dependencies are required")
	}
	return &Service{repository: repository, storage: storage, authorizer: authorizer, clock: systemClock, presignTTL: presignTTL, maxBytes: maxBytes, maxAttempts: maxAttempts}, nil
}

type CreateBatchInput struct {
	CompanyID      organization.CompanyID
	Operation      domain.Operation
	Filename       string
	Extension      string
	MIMEType       string
	ExpectedSize   int64
	TemplateID     *domain.TemplateID
	IdempotencyKey *string
	PayrollContext *domain.PayrollImportContext
}

type CreateBatchResult struct {
	Batch         domain.ImportBatch
	File          domain.FileObject
	UploadSession platformstorage.UploadSession
}

func (service *Service) CreateBatch(ctx context.Context, input CreateBatchInput) (CreateBatchResult, error) {
	// The batch and its pending input file are created together so an upload
	// session cannot exist without the import workflow that owns it.
	if err := service.authorizer.Require(ctx, security.CapabilityFileBatchCreate); err != nil {
		return CreateBatchResult{}, err
	}
	if err := service.authorizer.RequireCompany(ctx, input.CompanyID.UUID()); err != nil {
		return CreateBatchResult{}, err
	}
	tenantUUID, err := security.ActiveTenant(ctx, service.authorizer)
	if err != nil {
		return CreateBatchResult{}, err
	}
	userID, err := security.CurrentUserID(ctx)
	if err != nil {
		return CreateBatchResult{}, err
	}
	extension := normalizeExtension(input.Extension)
	if !supportedExtension(extension) {
		return CreateBatchResult{}, fmt.Errorf("%w: %s", domain.ErrUnsupportedFileFormat, extension)
	}
	filename := filepath.Base(strings.TrimSpace(input.Filename))
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		return CreateBatchResult{}, fmt.Errorf("%w: filename is required", domain.ErrInvalidFileObject)
	}
	if input.ExpectedSize < 0 || input.ExpectedSize > service.maxBytes {
		return CreateBatchResult{}, fmt.Errorf("%w: declared file size must be between zero and %d bytes", domain.ErrInvalidFileObject, service.maxBytes)
	}
	if input.TemplateID != nil {
		template, templateErr := service.repository.GetTemplate(ctx, *input.TemplateID)
		if templateErr != nil {
			return CreateBatchResult{}, templateErr
		}
		if templateErr := validateTemplateScope(template, tenantUUID, input.CompanyID.UUID(), extension); templateErr != nil {
			return CreateBatchResult{}, templateErr
		}
	}
	fileID, err := domain.NewFileObjectID()
	if err != nil {
		return CreateBatchResult{}, err
	}
	batchID, err := domain.NewBatchID()
	if err != nil {
		return CreateBatchResult{}, err
	}
	tenantID := organization.TenantID(tenantUUID)
	fileKey := fmt.Sprintf("tenants/%s/companies/%s/batches/%s/input.%s", tenantID.String(), input.CompanyID.String(), batchID.String(), extension)
	file := domain.FileObject{
		ID:                fileID,
		TenantID:          tenantID,
		CompanyID:         input.CompanyID,
		StorageProvider:   "filesystem",
		ObjectKey:         fileKey,
		OriginalFilename:  filename,
		DetectedExtension: extension,
		DetectedMIMEType:  strings.TrimSpace(input.MIMEType),
		SizeBytes:         input.ExpectedSize,
		SHA256:            make([]byte, 32),
		EncryptionMode:    "filesystem-private",
		Status:            domain.FileObjectPending,
		CreatedBy:         userID,
		CreatedAt:         service.clock.Now().UTC(),
		UpdatedAt:         service.clock.Now().UTC(),
	}
	batch, err := domain.NewImportBatch(batchID, tenantID, input.CompanyID, input.Operation, fileID, userID, service.clock.Now())
	if err != nil {
		return CreateBatchResult{}, err
	}
	batch.TemplateID = input.TemplateID
	if input.Operation == domain.OperationPayrollLedger {
		if input.PayrollContext == nil {
			return CreateBatchResult{}, fmt.Errorf("%w: payroll context is required", domain.ErrInvalidBatch)
		}
		if err := input.PayrollContext.Validate(); err != nil {
			return CreateBatchResult{}, err
		}
		batch.PayrollContext = input.PayrollContext
	}
	upload, err := service.storage.PrepareUpload(ctx, file.ObjectKey, file.DetectedMIMEType, service.presignTTL)
	if err != nil {
		return CreateBatchResult{}, err
	}
	file.StorageProvider = upload.Provider
	if upload.Provider == "s3" {
		file.EncryptionMode = "sse-s3"
	}
	if _, err := service.repository.CreateBatch(ctx, file, batch); err != nil {
		return CreateBatchResult{}, err
	}
	return CreateBatchResult{Batch: batch, File: file, UploadSession: upload}, nil
}

func (service *Service) RequestCommit(ctx context.Context, id domain.BatchID) (domain.ImportBatch, jobdomain.Job, error) {
	if err := service.authorizer.Require(ctx, security.CapabilityFileBatchCommit); err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	batch, err := service.GetBatch(ctx, id)
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	_, err = batch.RequestCommit(service.clock.Now())
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	jobID, err := jobdomain.NewID()
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	payload, err := json.Marshal(map[string]string{"batchId": batch.ID.String()})
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	idempotencyKey := "commit:" + batch.ID.String()
	job, err := jobdomain.New(jobID, &batch.TenantID, &batch.CompanyID, string(batch.Operation)+".commit", "file-commit", payload, &idempotencyKey, service.clock.Now())
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	job.MaxAttempts = service.maxAttempts
	stepID, err := domain.NewStepID()
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	return service.repository.RequestCommitWithJob(ctx, CommitRequest{BatchID: batch.ID, Job: job, StepID: stepID})
}

func (service *Service) CompleteUpload(ctx context.Context, batchID domain.BatchID) (domain.ImportBatch, jobdomain.Job, error) {
	if err := service.authorizer.Require(ctx, security.CapabilityFileBatchCreate); err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	batch, err := service.repository.GetBatch(ctx, batchID)
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	if err := service.requireBatchAccess(ctx, batch); err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	file, err := service.repository.GetFileObject(ctx, batch.InputFileID)
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	info, err := service.storage.CompleteUpload(ctx, file.ObjectKey)
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	if file.SizeBytes > 0 && file.SizeBytes != info.SizeBytes {
		return domain.ImportBatch{}, jobdomain.Job{}, fmt.Errorf("%w: uploaded size does not match declared size", domain.ErrInvalidFileObject)
	}
	if info.SizeBytes > service.maxBytes {
		return domain.ImportBatch{}, jobdomain.Job{}, fmt.Errorf("%w: uploaded file exceeds maximum size", domain.ErrInvalidFileObject)
	}
	return service.enqueueValidation(ctx, batch, file, info)
}

func (service *Service) UploadDirect(ctx context.Context, batchID domain.BatchID, input io.Reader, size int64, mimeType string) (domain.ImportBatch, jobdomain.Job, error) {
	if err := service.authorizer.Require(ctx, security.CapabilityFileBatchCreate); err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	if size < 0 || size > service.maxBytes {
		return domain.ImportBatch{}, jobdomain.Job{}, fmt.Errorf("%w: upload size is invalid", domain.ErrInvalidFileObject)
	}
	batch, err := service.repository.GetBatch(ctx, batchID)
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	if err := service.requireBatchAccess(ctx, batch); err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	file, err := service.repository.GetFileObject(ctx, batch.InputFileID)
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	if file.SizeBytes > 0 && file.SizeBytes != size {
		return domain.ImportBatch{}, jobdomain.Job{}, fmt.Errorf("%w: uploaded size does not match declared size", domain.ErrInvalidFileObject)
	}
	info, err := service.storage.Put(ctx, file.ObjectKey, io.LimitReader(input, service.maxBytes+1), size, mimeType)
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	if info.SizeBytes != size {
		return domain.ImportBatch{}, jobdomain.Job{}, fmt.Errorf("%w: uploaded size does not match content length", domain.ErrInvalidFileObject)
	}
	return service.enqueueValidation(ctx, batch, file, info)
}

func (service *Service) enqueueValidation(ctx context.Context, batch domain.ImportBatch, file domain.FileObject, info platformstorage.ObjectInfo) (domain.ImportBatch, jobdomain.Job, error) {
	jobID, err := jobdomain.NewID()
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	payload, err := json.Marshal(map[string]string{"batchId": batch.ID.String()})
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	idempotencyKey := "validate:" + batch.ID.String()
	job, err := jobdomain.New(jobID, &batch.TenantID, &batch.CompanyID, string(batch.Operation)+".validate", "file-validation", payload, &idempotencyKey, service.clock.Now())
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	job.MaxAttempts = service.maxAttempts
	stepID, err := domain.NewStepID()
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	completion := UploadCompletion{BatchID: batch.ID, FileObjectID: file.ID, SizeBytes: info.SizeBytes, SHA256: info.SHA256, ValidationJob: job, StepID: stepID}
	completed, createdJob, err := service.repository.CompleteUploadWithValidationJob(ctx, completion)
	if err != nil {
		return domain.ImportBatch{}, jobdomain.Job{}, err
	}
	return completed, createdJob, nil
}

func (service *Service) GetBatch(ctx context.Context, id domain.BatchID) (domain.ImportBatch, error) {
	if err := service.authorizer.Require(ctx, security.CapabilityFileBatchRead); err != nil {
		return domain.ImportBatch{}, err
	}
	batch, err := service.repository.GetBatch(ctx, id)
	if err != nil {
		return domain.ImportBatch{}, err
	}
	if err := service.requireBatchAccess(ctx, batch); err != nil {
		return domain.ImportBatch{}, err
	}
	return batch, nil
}

func (service *Service) ListTemplates(ctx context.Context) ([]domain.ImportTemplate, error) {
	if err := service.authorizer.Require(ctx, security.CapabilityFileTemplateRead); err != nil {
		return nil, err
	}
	tenantID, err := security.ActiveTenant(ctx, service.authorizer)
	if err != nil {
		return nil, err
	}
	return service.repository.ListTemplates(ctx, tenantID)
}

func (service *Service) GetTemplate(ctx context.Context, id domain.TemplateID) (domain.ImportTemplate, error) {
	if err := service.authorizer.Require(ctx, security.CapabilityFileTemplateRead); err != nil {
		return domain.ImportTemplate{}, err
	}
	tenantID, err := security.ActiveTenant(ctx, service.authorizer)
	if err != nil {
		return domain.ImportTemplate{}, err
	}
	template, err := service.repository.GetTemplate(ctx, id)
	if err != nil {
		return domain.ImportTemplate{}, err
	}
	if template.TenantID != nil && template.TenantID.UUID() != tenantID {
		return domain.ImportTemplate{}, security.ErrForbidden
	}
	return template, nil
}

type CreateTemplateInput struct {
	CompanyID     organization.CompanyID
	TemplateType  string
	Version       string
	FileFormat    string
	Configuration json.RawMessage
}

func (service *Service) CreateTemplate(ctx context.Context, input CreateTemplateInput) (domain.ImportTemplate, error) {
	if err := service.authorizer.Require(ctx, security.CapabilityFileTemplateWrite); err != nil {
		return domain.ImportTemplate{}, err
	}
	tenantUUID, err := security.ActiveTenant(ctx, service.authorizer)
	if err != nil {
		return domain.ImportTemplate{}, err
	}
	if err := service.authorizer.RequireCompany(ctx, input.CompanyID.UUID()); err != nil {
		return domain.ImportTemplate{}, err
	}
	if len(input.Configuration) == 0 {
		input.Configuration = json.RawMessage(`{}`)
	}
	var object map[string]any
	if err := json.Unmarshal(input.Configuration, &object); err != nil || object == nil {
		return domain.ImportTemplate{}, domain.ErrInvalidTemplate
	}
	id, err := domain.NewTemplateID()
	if err != nil {
		return domain.ImportTemplate{}, err
	}
	template := domain.ImportTemplate{ID: id, TenantID: tenantIDPtr(organization.TenantID(tenantUUID)), CompanyID: companyIDPtr(input.CompanyID), TemplateType: strings.TrimSpace(input.TemplateType), Version: strings.TrimSpace(input.Version), FileFormat: normalizeExtension(input.FileFormat), Status: "active", Configuration: input.Configuration}
	if template.TemplateType == "" || template.Version == "" || !supportedExtension(template.FileFormat) {
		return domain.ImportTemplate{}, domain.ErrInvalidTemplate
	}
	return service.repository.CreateTemplate(ctx, template)
}

func (service *Service) RetireTemplate(ctx context.Context, id domain.TemplateID) error {
	if err := service.authorizer.Require(ctx, security.CapabilityFileTemplateWrite); err != nil {
		return err
	}
	template, err := service.GetTemplate(ctx, id)
	if err != nil {
		return err
	}
	if template.Status != "active" {
		return domain.ErrTemplateNotApplicable
	}
	return service.repository.RetireTemplate(ctx, id)
}

func tenantIDPtr(value organization.TenantID) *organization.TenantID    { return &value }
func companyIDPtr(value organization.CompanyID) *organization.CompanyID { return &value }

func validateTemplateScope(template domain.ImportTemplate, tenantID, companyID uuid.UUID, extension string) error {
	if template.TenantID != nil && template.TenantID.UUID() != tenantID {
		return security.ErrForbidden
	}
	if template.CompanyID != nil && template.CompanyID.UUID() != companyID {
		return security.ErrForbidden
	}
	if template.FileFormat != extension {
		return fmt.Errorf("%w: template requires %s, received %s", domain.ErrTemplateNotApplicable, template.FileFormat, extension)
	}
	if template.Status != "active" {
		return domain.ErrTemplateNotApplicable
	}
	return nil
}

func (service *Service) RequestCancel(ctx context.Context, id domain.BatchID) (domain.ImportBatch, error) {
	if err := service.authorizer.Require(ctx, security.CapabilityFileBatchCancel); err != nil {
		return domain.ImportBatch{}, err
	}
	batch, err := service.GetBatch(ctx, id)
	if err != nil {
		return domain.ImportBatch{}, err
	}
	_, err = batch.RequestCancel(service.clock.Now())
	if err != nil {
		return domain.ImportBatch{}, err
	}
	return service.repository.RequestCancellation(ctx, id)
}

func (service *Service) ListArtifacts(ctx context.Context, id domain.BatchID) ([]Artifact, error) {
	if _, err := service.GetBatch(ctx, id); err != nil {
		return nil, err
	}
	return service.repository.ListArtifacts(ctx, id)
}

func (service *Service) ListValidationSheets(ctx context.Context, id domain.BatchID) ([]ValidationSheet, error) {
	if _, err := service.GetBatch(ctx, id); err != nil {
		return nil, err
	}
	return service.repository.ListValidationSheets(ctx, id)
}

func (service *Service) ListValidationIssues(ctx context.Context, id domain.BatchID) ([]ValidationIssueView, error) {
	if _, err := service.GetBatch(ctx, id); err != nil {
		return nil, err
	}
	return service.repository.ListValidationIssues(ctx, id)
}

func (service *Service) ResolveValidationRow(ctx context.Context, batchID domain.BatchID, rowID domain.ImportRowID, employeeID, employmentID *uuid.UUID, reason string) error {
	if err := service.authorizer.Require(ctx, security.CapabilityFileBatchCommit); err != nil {
		return err
	}
	if _, err := service.GetBatch(ctx, batchID); err != nil {
		return err
	}
	if employeeID == nil {
		return fmt.Errorf("%w: employee mapping is required", domain.ErrInvalidBatch)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: resolution reason is required", domain.ErrInvalidBatch)
	}
	userID, err := security.CurrentUserID(ctx)
	if err != nil {
		return err
	}
	return service.repository.ResolveValidationRow(ctx, ResolveValidationRowInput{
		BatchID: batchID, RowID: rowID, EmployeeID: employeeID, EmploymentID: employmentID,
		Decision: "map", Reason: strings.TrimSpace(reason), DecidedBy: userID.UUID(),
	})
}

func (service *Service) SignedArtifactURL(ctx context.Context, id domain.BatchID, artifactID domain.ArtifactID) (string, time.Time, error) {
	if err := service.authorizer.Require(ctx, security.CapabilityFileArtifactRead); err != nil {
		return "", time.Time{}, err
	}
	if _, err := service.GetBatch(ctx, id); err != nil {
		return "", time.Time{}, err
	}
	artifacts, err := service.repository.ListArtifacts(ctx, id)
	if err != nil {
		return "", time.Time{}, err
	}
	for _, artifact := range artifacts {
		if artifact.ID == artifactID {
			url, err := service.storage.PresignGet(ctx, artifact.FileObject.ObjectKey, service.presignTTL)
			if err != nil {
				return "", time.Time{}, err
			}
			return url, service.clock.Now().UTC().Add(service.presignTTL), nil
		}
	}
	return "", time.Time{}, domain.ErrFileObjectNotFound
}

func (service *Service) requireBatchAccess(ctx context.Context, batch domain.ImportBatch) error {
	tenantID, err := security.ActiveTenant(ctx, service.authorizer)
	if err != nil {
		return err
	}
	if tenantID != batch.TenantID.UUID() {
		return security.ErrForbidden
	}
	return service.authorizer.RequireCompany(ctx, batch.CompanyID.UUID())
}

func supportedExtension(extension string) bool {
	switch extension {
	case "xlsx", "xls", "xlsm", "xlm", "csv", "json":
		return true
	default:
		return false
	}
}

func normalizeExtension(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), ".")
}
