package e2e

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	appfilebatch "github.com/navyaraksha/imogi/internal/application/filebatch"
	filedomain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
	jobdomain "github.com/navyaraksha/imogi/internal/domain/job"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	fileparser "github.com/navyaraksha/imogi/internal/infrastructure/fileparser"
	objectstorage "github.com/navyaraksha/imogi/internal/infrastructure/objectstorage"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

func TestImportWorkflowMasterAndPayrollReachCommitRequestedAfterValidation(t *testing.T) {
	tenantID := organization.TenantID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-111111111111"))
	companyID := organization.CompanyID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-222222222222"))
	userID := identitydomain.UserID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-333333333333"))
	employeeID := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-444444444444")

	ctx := security.WithPrincipal(context.Background(), security.Principal{
		UserID: userID.UUID(), Subject: "e2e-import-user", TenantID: tenantID.UUID(),
		Capabilities: allFileCapabilities(), CompanyIDs: map[uuid.UUID]struct{}{companyID.UUID(): {}},
	})
	authorizer := security.ContextAuthorizer{}
	repository := newImportRepository()
	storage, err := objectstorage.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parser, err := fileparser.New(100)
	if err != nil {
		t.Fatal(err)
	}
	systemClock := fixedClock{now: time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)}
	service, err := appfilebatch.NewService(repository, storage, authorizer, systemClock, time.Hour, 10<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := appfilebatch.NewValidator(repository, storage, parser, systemClock, 100)
	if err != nil {
		t.Fatal(err)
	}
	validator.SetIdentityMatcher(importIdentityMatcher{candidates: []appfilebatch.IdentityCandidate{{
		EmployeeID: employeeID, EmployeeNumber: "EMP-001", FullName: "Ardianto",
	}}})

	t.Run("employee master", func(t *testing.T) {
		batch, validationJob := createAndUpload(t, ctx, service, companyID, filedomain.OperationEmployeeMaster, "master.csv", "NIK,Nama,No Karyawan\n3174010101010001, Karyawan Baru ,NF001\n")
		if err := validator.Handle(ctx, validationJob); err != nil {
			t.Fatal(err)
		}
		validated := mustGetBatch(t, repository, batch.ID)
		assertValidationPassed(t, validated, 1)
		assertReadyImportArtifact(t, repository, batch.ID)

		committed, commitJob, err := service.RequestCommit(ctx, batch.ID)
		if err != nil {
			t.Fatal(err)
		}
		if committed.Status != filedomain.BatchCommitRequested {
			t.Fatalf("status after commit request = %q", committed.Status)
		}
		if commitJob.JobType != "employee_master.commit" || commitJob.QueueName != "file-commit" {
			t.Fatalf("unexpected commit job: type=%q queue=%q", commitJob.JobType, commitJob.QueueName)
		}
	})

	t.Run("payroll ledger", func(t *testing.T) {
		batch, validationJob := createAndUpload(t, ctx, service, companyID, filedomain.OperationPayrollLedger, "payroll.csv", "employee_number,full_name,gross_income,taxable_income,take_home_pay\nEMP-001,Ardianto,10000000,9000000,8500000\n")
		if err := validator.Handle(ctx, validationJob); err != nil {
			t.Fatal(err)
		}
		validated := mustGetBatch(t, repository, batch.ID)
		assertValidationPassed(t, validated, 0)
		assertReadyImportArtifact(t, repository, batch.ID)

		committed, commitJob, err := service.RequestCommit(ctx, batch.ID)
		if err != nil {
			t.Fatal(err)
		}
		if committed.Status != filedomain.BatchCommitRequested {
			t.Fatalf("status after commit request = %q", committed.Status)
		}
		if commitJob.JobType != "payroll_ledger.commit" || commitJob.QueueName != "file-commit" {
			t.Fatalf("unexpected commit job: type=%q queue=%q", commitJob.JobType, commitJob.QueueName)
		}
	})
}

func createAndUpload(t *testing.T, ctx context.Context, service *appfilebatch.Service, companyID organization.CompanyID, operation filedomain.Operation, filename, content string) (filedomain.ImportBatch, jobdomain.Job) {
	t.Helper()
	var payrollContext *filedomain.PayrollImportContext
	if operation == filedomain.OperationPayrollLedger {
		from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
		payrollContext = &filedomain.PayrollImportContext{TaxYear: 2026, TaxMonth: 1, CoverageFrom: from, CoverageTo: to, RunType: "regular"}
	}
	created, err := service.CreateBatch(ctx, appfilebatch.CreateBatchInput{
		CompanyID: companyID, Operation: operation, Filename: filename, Extension: "csv",
		MIMEType: "text/csv", ExpectedSize: int64(len(content)),
		PayrollContext: payrollContext,
	})
	if err != nil {
		t.Fatal(err)
	}
	batch, validationJob, err := service.UploadDirect(ctx, created.Batch.ID, strings.NewReader(content), int64(len(content)), "text/csv")
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != filedomain.BatchUploaded {
		t.Fatalf("status after upload = %q", batch.Status)
	}
	return batch, validationJob
}

func assertValidationPassed(t *testing.T, batch filedomain.ImportBatch, warnings int) {
	t.Helper()
	if batch.Status != filedomain.BatchValidated {
		t.Fatalf("validation status = %q", batch.Status)
	}
	if batch.InvalidRows != 0 {
		t.Fatalf("invalid rows = %d", batch.InvalidRows)
	}
	if batch.ValidRows != 1 {
		t.Fatalf("valid rows = %d", batch.ValidRows)
	}
	if batch.WarningRows != warnings {
		t.Fatalf("warning rows = %d, want %d", batch.WarningRows, warnings)
	}
}

func assertReadyImportArtifact(t *testing.T, repository *importRepository, batchID filedomain.BatchID) {
	t.Helper()
	for _, artifact := range repository.artifacts[batchID] {
		if artifact.Role == filedomain.ArtifactReadyImport {
			return
		}
	}
	t.Fatalf("batch %s has no ready-import artifact", batchID)
}

func mustGetBatch(t *testing.T, repository *importRepository, id filedomain.BatchID) filedomain.ImportBatch {
	t.Helper()
	batch, err := repository.GetBatch(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func allFileCapabilities() map[string]struct{} {
	return map[string]struct{}{
		security.CapabilityFileBatchCreate: {}, security.CapabilityFileBatchRead: {},
		security.CapabilityFileBatchCommit: {}, security.CapabilityFileArtifactRead: {},
		security.CapabilityFileTemplateRead: {},
	}
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

var _ clock.Clock = fixedClock{}

type importIdentityMatcher struct {
	candidates []appfilebatch.IdentityCandidate
}

func (matcher importIdentityMatcher) FindIdentityCandidates(context.Context, uuid.UUID, uuid.UUID, string, string, string) ([]appfilebatch.IdentityCandidate, error) {
	return matcher.candidates, nil
}

type importRepository struct {
	files     map[filedomain.FileObjectID]filedomain.FileObject
	batches   map[filedomain.BatchID]filedomain.ImportBatch
	jobs      map[jobdomain.ID]jobdomain.Job
	artifacts map[filedomain.BatchID]map[filedomain.ArtifactRole]appfilebatch.Artifact
	rows      map[filedomain.BatchID][]appfilebatch.ValidationRow
}

func newImportRepository() *importRepository {
	return &importRepository{
		files: make(map[filedomain.FileObjectID]filedomain.FileObject), batches: make(map[filedomain.BatchID]filedomain.ImportBatch),
		jobs: make(map[jobdomain.ID]jobdomain.Job), artifacts: make(map[filedomain.BatchID]map[filedomain.ArtifactRole]appfilebatch.Artifact),
		rows: make(map[filedomain.BatchID][]appfilebatch.ValidationRow),
	}
}

func (repository *importRepository) CreateBatch(_ context.Context, file filedomain.FileObject, batch filedomain.ImportBatch) (filedomain.ImportBatch, error) {
	repository.files[file.ID] = file
	repository.batches[batch.ID] = batch
	return batch, nil
}

func (repository *importRepository) SavePayrollContext(_ context.Context, id filedomain.BatchID, value filedomain.PayrollImportContext) error {
	batch := repository.batches[id]
	batch.PayrollContext = &value
	repository.batches[id] = batch
	return nil
}

func (repository *importRepository) GetBatch(_ context.Context, id filedomain.BatchID) (filedomain.ImportBatch, error) {
	batch, ok := repository.batches[id]
	if !ok {
		return filedomain.ImportBatch{}, filedomain.ErrBatchNotFound
	}
	return batch, nil
}

func (repository *importRepository) GetFileObject(_ context.Context, id filedomain.FileObjectID) (filedomain.FileObject, error) {
	file, ok := repository.files[id]
	if !ok {
		return filedomain.FileObject{}, filedomain.ErrFileObjectNotFound
	}
	return file, nil
}

func (repository *importRepository) ListTemplates(context.Context, uuid.UUID) ([]filedomain.ImportTemplate, error) {
	return nil, nil
}

func (repository *importRepository) GetTemplate(context.Context, filedomain.TemplateID) (filedomain.ImportTemplate, error) {
	return filedomain.ImportTemplate{}, filedomain.ErrTemplateNotFound
}

func (repository *importRepository) CreateTemplate(_ context.Context, template filedomain.ImportTemplate) (filedomain.ImportTemplate, error) {
	return template, nil
}
func (repository *importRepository) RetireTemplate(context.Context, filedomain.TemplateID) error {
	return nil
}

func (repository *importRepository) CreateArtifactFile(_ context.Context, file filedomain.FileObject, batchID filedomain.BatchID, artifactID filedomain.ArtifactID, role filedomain.ArtifactRole) error {
	repository.files[file.ID] = file
	if repository.artifacts[batchID] == nil {
		repository.artifacts[batchID] = make(map[filedomain.ArtifactRole]appfilebatch.Artifact)
	}
	repository.artifacts[batchID][role] = appfilebatch.Artifact{ID: artifactID, BatchID: batchID, FileObject: file, Role: role, CreatedAt: file.CreatedAt}
	return nil
}

func (repository *importRepository) CompleteUploadWithValidationJob(_ context.Context, completion appfilebatch.UploadCompletion) (filedomain.ImportBatch, jobdomain.Job, error) {
	file := repository.files[completion.FileObjectID]
	file.Status = filedomain.FileObjectAvailable
	file.SizeBytes = completion.SizeBytes
	file.SHA256 = append([]byte(nil), completion.SHA256...)
	repository.files[file.ID] = file
	batch := repository.batches[completion.BatchID]
	batch.Status = filedomain.BatchUploaded
	repository.batches[batch.ID] = batch
	repository.jobs[completion.ValidationJob.ID] = completion.ValidationJob
	return batch, completion.ValidationJob, nil
}

func (repository *importRepository) RequestCommitWithJob(_ context.Context, request appfilebatch.CommitRequest) (filedomain.ImportBatch, jobdomain.Job, error) {
	batch := repository.batches[request.BatchID]
	if batch.Status != filedomain.BatchValidated || batch.InvalidRows != 0 {
		return filedomain.ImportBatch{}, jobdomain.Job{}, filedomain.ErrBatchValidationRequired
	}
	batch.Status = filedomain.BatchCommitRequested
	repository.batches[batch.ID] = batch
	repository.jobs[request.Job.ID] = request.Job
	return batch, request.Job, nil
}

func (repository *importRepository) RequestCancellation(_ context.Context, id filedomain.BatchID) (filedomain.ImportBatch, error) {
	batch := repository.batches[id]
	batch.Status = filedomain.BatchCancelRequested
	repository.batches[id] = batch
	return batch, nil
}

func (repository *importRepository) UpdateBatchStatus(_ context.Context, update appfilebatch.BatchStatusUpdate) (filedomain.ImportBatch, error) {
	batch, ok := repository.batches[update.BatchID]
	if !ok {
		return filedomain.ImportBatch{}, filedomain.ErrBatchNotFound
	}
	batch.Status = update.Status
	if update.TotalRows != nil {
		batch.TotalRows = *update.TotalRows
	}
	if update.ValidRows != nil {
		batch.ValidRows = *update.ValidRows
	}
	if update.InvalidRows != nil {
		batch.InvalidRows = *update.InvalidRows
	}
	if update.WarningRows != nil {
		batch.WarningRows = *update.WarningRows
	}
	if update.CommittedRows != nil {
		batch.CommittedRows = *update.CommittedRows
	}
	if update.RejectedRows != nil {
		batch.RejectedRows = *update.RejectedRows
	}
	if update.ValidationStartedAt != nil {
		batch.ValidationStartedAt = update.ValidationStartedAt
	}
	if update.ValidationFinishedAt != nil {
		batch.ValidationFinishedAt = update.ValidationFinishedAt
	}
	if update.CommitStartedAt != nil {
		batch.CommitStartedAt = update.CommitStartedAt
	}
	if update.CommitFinishedAt != nil {
		batch.CommitFinishedAt = update.CommitFinishedAt
	}
	repository.batches[batch.ID] = batch
	return batch, nil
}

func (repository *importRepository) CreateArtifact(_ context.Context, id filedomain.ArtifactID, batchID filedomain.BatchID, fileID filedomain.FileObjectID, role filedomain.ArtifactRole) error {
	file := repository.files[fileID]
	return repository.CreateArtifactFile(context.Background(), file, batchID, id, role)
}

func (repository *importRepository) ListArtifacts(_ context.Context, batchID filedomain.BatchID) ([]appfilebatch.Artifact, error) {
	items := make([]appfilebatch.Artifact, 0, len(repository.artifacts[batchID]))
	for _, artifact := range repository.artifacts[batchID] {
		items = append(items, artifact)
	}
	return items, nil
}

func (repository *importRepository) ClearValidationRows(_ context.Context, batchID filedomain.BatchID) error {
	delete(repository.rows, batchID)
	return nil
}

func (repository *importRepository) CreateValidationRow(_ context.Context, row appfilebatch.ValidationRow) error {
	repository.rows[row.BatchID] = append(repository.rows[row.BatchID], row)
	return nil
}

func (repository *importRepository) ListValidationSheets(context.Context, filedomain.BatchID) ([]appfilebatch.ValidationSheet, error) {
	return nil, nil
}

func (repository *importRepository) ListValidationIssues(context.Context, filedomain.BatchID) ([]appfilebatch.ValidationIssueView, error) {
	return nil, nil
}

func (repository *importRepository) ResolveValidationRow(context.Context, appfilebatch.ResolveValidationRowInput) error {
	return errors.New("not used in import workflow e2e")
}

func (repository *importRepository) ListValidationRows(_ context.Context, batchID filedomain.BatchID) ([]appfilebatch.ValidationRow, error) {
	return append([]appfilebatch.ValidationRow(nil), repository.rows[batchID]...), nil
}

func (repository *importRepository) CreateImportRowEffect(context.Context, appfilebatch.ImportRowEffect) error {
	return nil
}

func (repository *importRepository) LinkPayrollContext(context.Context, filedomain.BatchID, uuid.UUID, uuid.UUID) error {
	return nil
}

var _ appfilebatch.Repository = (*importRepository)(nil)
var _ appfilebatch.IdentityMatcher = importIdentityMatcher{}
