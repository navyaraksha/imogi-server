package preview

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"

	appfilebatch "github.com/navyaraksha/imogi/internal/application/filebatch"
	filedomain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	jobdomain "github.com/navyaraksha/imogi/internal/domain/job"
)

type memoryRepository struct {
	mu        sync.Mutex
	files     map[filedomain.FileObjectID]filedomain.FileObject
	batches   map[filedomain.BatchID]filedomain.ImportBatch
	templates map[filedomain.TemplateID]filedomain.ImportTemplate
	artifacts map[filedomain.BatchID][]appfilebatch.Artifact
	rows      map[filedomain.BatchID][]appfilebatch.ValidationRow
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		files:     make(map[filedomain.FileObjectID]filedomain.FileObject),
		batches:   make(map[filedomain.BatchID]filedomain.ImportBatch),
		templates: make(map[filedomain.TemplateID]filedomain.ImportTemplate),
		artifacts: make(map[filedomain.BatchID][]appfilebatch.Artifact),
		rows:      make(map[filedomain.BatchID][]appfilebatch.ValidationRow),
	}
}

func (repository *memoryRepository) CreateBatch(_ context.Context, file filedomain.FileObject, batch filedomain.ImportBatch) (filedomain.ImportBatch, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.files[file.ID] = file
	repository.batches[batch.ID] = batch
	return batch, nil
}

func (repository *memoryRepository) SavePayrollContext(_ context.Context, id filedomain.BatchID, value filedomain.PayrollImportContext) error {
	batch := repository.batches[id]
	batch.PayrollContext = &value
	repository.batches[id] = batch
	return nil
}

func (repository *memoryRepository) GetBatch(_ context.Context, id filedomain.BatchID) (filedomain.ImportBatch, error) {
	value, ok := repository.batches[id]
	if !ok {
		return filedomain.ImportBatch{}, filedomain.ErrBatchNotFound
	}
	return value, nil
}

func (repository *memoryRepository) GetFileObject(_ context.Context, id filedomain.FileObjectID) (filedomain.FileObject, error) {
	value, ok := repository.files[id]
	if !ok {
		return filedomain.FileObject{}, filedomain.ErrFileObjectNotFound
	}
	return value, nil
}

func (repository *memoryRepository) ListTemplates(context.Context, uuid.UUID) ([]filedomain.ImportTemplate, error) {
	return nil, nil
}

func (repository *memoryRepository) GetTemplate(_ context.Context, id filedomain.TemplateID) (filedomain.ImportTemplate, error) {
	value, ok := repository.templates[id]
	if !ok {
		return filedomain.ImportTemplate{}, filedomain.ErrTemplateNotFound
	}
	return value, nil
}

func (repository *memoryRepository) CreateTemplate(_ context.Context, value filedomain.ImportTemplate) (filedomain.ImportTemplate, error) {
	repository.templates[value.ID] = value
	return value, nil
}

func (repository *memoryRepository) RetireTemplate(context.Context, filedomain.TemplateID) error {
	return nil
}

func (repository *memoryRepository) CreateArtifactFile(_ context.Context, file filedomain.FileObject, batchID filedomain.BatchID, artifactID filedomain.ArtifactID, role filedomain.ArtifactRole) error {
	repository.files[file.ID] = file
	repository.artifacts[batchID] = append(repository.artifacts[batchID], appfilebatch.Artifact{
		ID: artifactID, BatchID: batchID, FileObject: file, Role: role, CreatedAt: file.CreatedAt,
	})
	return nil
}

func (repository *memoryRepository) CompleteUploadWithValidationJob(context.Context, appfilebatch.UploadCompletion) (filedomain.ImportBatch, jobdomain.Job, error) {
	return filedomain.ImportBatch{}, jobdomain.Job{}, errors.New("preview does not enqueue jobs")
}

func (repository *memoryRepository) RequestCommitWithJob(context.Context, appfilebatch.CommitRequest) (filedomain.ImportBatch, jobdomain.Job, error) {
	return filedomain.ImportBatch{}, jobdomain.Job{}, errors.New("preview does not commit")
}

func (repository *memoryRepository) RequestCancellation(context.Context, filedomain.BatchID) (filedomain.ImportBatch, error) {
	return filedomain.ImportBatch{}, errors.New("preview does not cancel")
}

func (repository *memoryRepository) UpdateBatchStatus(_ context.Context, update appfilebatch.BatchStatusUpdate) (filedomain.ImportBatch, error) {
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
	if update.ValidationStartedAt != nil {
		batch.ValidationStartedAt = update.ValidationStartedAt
	}
	if update.ValidationFinishedAt != nil {
		batch.ValidationFinishedAt = update.ValidationFinishedAt
	}
	repository.batches[batch.ID] = batch
	return batch, nil
}

func (repository *memoryRepository) CreateArtifact(_ context.Context, id filedomain.ArtifactID, batchID filedomain.BatchID, fileID filedomain.FileObjectID, role filedomain.ArtifactRole) error {
	file, ok := repository.files[fileID]
	if !ok {
		return filedomain.ErrFileObjectNotFound
	}
	return repository.CreateArtifactFile(context.Background(), file, batchID, id, role)
}

func (repository *memoryRepository) ListArtifacts(_ context.Context, id filedomain.BatchID) ([]appfilebatch.Artifact, error) {
	return append([]appfilebatch.Artifact(nil), repository.artifacts[id]...), nil
}

func (repository *memoryRepository) ClearValidationRows(_ context.Context, id filedomain.BatchID) error {
	delete(repository.rows, id)
	return nil
}

func (repository *memoryRepository) CreateValidationRow(_ context.Context, row appfilebatch.ValidationRow) error {
	repository.rows[row.BatchID] = append(repository.rows[row.BatchID], row)
	return nil
}

func (repository *memoryRepository) ListValidationSheets(context.Context, filedomain.BatchID) ([]appfilebatch.ValidationSheet, error) {
	return nil, nil
}

func (repository *memoryRepository) ListValidationIssues(context.Context, filedomain.BatchID) ([]appfilebatch.ValidationIssueView, error) {
	return nil, nil
}

func (repository *memoryRepository) ResolveValidationRow(context.Context, appfilebatch.ResolveValidationRowInput) error {
	return errors.New("preview does not resolve rows")
}

func (repository *memoryRepository) ListValidationRows(_ context.Context, id filedomain.BatchID) ([]appfilebatch.ValidationRow, error) {
	return append([]appfilebatch.ValidationRow(nil), repository.rows[id]...), nil
}

func (repository *memoryRepository) CreateImportRowEffect(context.Context, appfilebatch.ImportRowEffect) error {
	return nil
}

func (repository *memoryRepository) LinkPayrollContext(context.Context, filedomain.BatchID, uuid.UUID, uuid.UUID) error {
	return nil
}

var _ appfilebatch.Repository = (*memoryRepository)(nil)
