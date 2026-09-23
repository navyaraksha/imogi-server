package restapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	appfilebatch "github.com/navyaraksha/imogi/internal/application/filebatch"
	domain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	generated "github.com/navyaraksha/imogi/internal/restapi/generated"
)

// FileBatchHandler maps the asynchronous file workflow to the generated REST
// contract. Validation and commit remain worker use cases, not HTTP logic.
type FileBatchHandler struct {
	generated.Unimplemented
	service *appfilebatch.Service
}

func NewFileBatchHandler(service *appfilebatch.Service) (*FileBatchHandler, error) {
	if service == nil {
		return nil, errors.New("file batch service is required")
	}
	return &FileBatchHandler{service: service}, nil
}

func (handler *FileBatchHandler) CreateImportBatch(w http.ResponseWriter, r *http.Request, _ generated.CreateImportBatchParams) {
	var body generated.CreateImportBatchJSONBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	companyID, err := parseCompanyID(body.CompanyId)
	if err != nil {
		writeError(w, err)
		return
	}
	batch, err := handler.service.CreateBatch(r.Context(), appfilebatch.CreateBatchInput{
		CompanyID:    companyID,
		Operation:    domain.Operation(body.Operation),
		Filename:     body.Filename,
		Extension:    string(body.Extension),
		MIMEType:     body.ContentType,
		ExpectedSize: body.ExpectedSize,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response := importBatchUploadResponse{Batch: importBatchResponseFromDomain(batch.Batch), Upload: uploadSessionResponse{Provider: batch.UploadSession.Provider, ObjectKey: batch.UploadSession.ObjectKey, Method: batch.UploadSession.Method, URL: batch.UploadSession.URL, Headers: batch.UploadSession.Headers}}
	w.Header().Set("Location", "/api/v1/import-batches/"+batch.Batch.ID.String())
	writeJSON(w, http.StatusCreated, response)
}

func (handler *FileBatchHandler) GetImportBatch(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.GetImportBatchParams) {
	batchID, err := parseFileBatchID(id)
	if err != nil {
		writeError(w, err)
		return
	}
	batch, err := handler.service.GetBatch(r.Context(), batchID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, importBatchResponseFromDomain(batch))
}

func (handler *FileBatchHandler) CompleteImportBatchUpload(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.CompleteImportBatchUploadParams) {
	batchID, err := parseFileBatchID(id)
	if err != nil {
		writeError(w, err)
		return
	}
	batch, job, err := handler.service.CompleteUpload(r.Context(), batchID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, importBatchJobResponse{Batch: importBatchResponseFromDomain(batch), JobID: job.ID.UUID()})
}

func (handler *FileBatchHandler) UploadImportBatchFile(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.UploadImportBatchFileParams) {
	batchID, err := parseFileBatchID(id)
	if err != nil {
		writeError(w, err)
		return
	}
	if r.ContentLength < 0 {
		writeError(w, errors.New("content length is required for direct file upload"))
		return
	}
	batch, job, err := handler.service.UploadDirect(r.Context(), batchID, r.Body, r.ContentLength, r.Header.Get("Content-Type"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, importBatchJobResponse{Batch: importBatchResponseFromDomain(batch), JobID: job.ID.UUID()})
}

func (handler *FileBatchHandler) CommitImportBatch(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.CommitImportBatchParams) {
	batchID, err := parseFileBatchID(id)
	if err != nil {
		writeError(w, err)
		return
	}
	batch, job, err := handler.service.RequestCommit(r.Context(), batchID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, importBatchJobResponse{Batch: importBatchResponseFromDomain(batch), JobID: job.ID.UUID()})
}

func (handler *FileBatchHandler) CancelImportBatch(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.CancelImportBatchParams) {
	batchID, err := parseFileBatchID(id)
	if err != nil {
		writeError(w, err)
		return
	}
	batch, err := handler.service.RequestCancel(r.Context(), batchID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, importBatchResponseFromDomain(batch))
}

func (handler *FileBatchHandler) ListImportBatchArtifacts(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.ListImportBatchArtifactsParams) {
	batchID, err := parseFileBatchID(id)
	if err != nil {
		writeError(w, err)
		return
	}
	artifacts, err := handler.service.ListArtifacts(r.Context(), batchID)
	if err != nil {
		writeError(w, err)
		return
	}
	data := make([]importBatchArtifactResponse, 0, len(artifacts))
	for _, artifact := range artifacts {
		data = append(data, importBatchArtifactResponse{ID: artifact.ID.UUID(), Role: string(artifact.Role), Filename: artifact.FileObject.OriginalFilename, Extension: artifact.FileObject.DetectedExtension, SizeBytes: artifact.FileObject.SizeBytes, CreatedAt: artifact.CreatedAt})
	}
	writeJSON(w, http.StatusOK, importBatchArtifactListResponse{Data: data})
}

func (handler *FileBatchHandler) GetImportBatchArtifactDownloadURL(w http.ResponseWriter, r *http.Request, batchIDValue openapi_types.UUID, artifactIDValue openapi_types.UUID, _ generated.GetImportBatchArtifactDownloadURLParams) {
	batchID, err := parseFileBatchID(batchIDValue)
	if err != nil {
		writeError(w, err)
		return
	}
	artifactID, err := parseFileArtifactID(artifactIDValue)
	if err != nil {
		writeError(w, err)
		return
	}
	url, expiresAt, err := handler.service.SignedArtifactURL(r.Context(), batchID, artifactID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, artifactDownloadURLResponse{URL: url, ExpiresAt: expiresAt})
}

func (handler *FileBatchHandler) ListImportTemplates(w http.ResponseWriter, r *http.Request, _ generated.ListImportTemplatesParams) {
	templates, err := handler.service.ListTemplates(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	data := make([]importTemplateResponse, 0, len(templates))
	for _, item := range templates {
		data = append(data, importTemplateResponse{ID: item.ID.UUID(), TemplateType: item.TemplateType, Version: item.Version, FileFormat: item.FileFormat, Status: item.Status})
	}
	writeJSON(w, http.StatusOK, importTemplateListResponse{Data: data})
}

type uploadSessionResponse struct {
	Provider  string            `json:"provider"`
	ObjectKey string            `json:"objectKey"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
}

type importBatchUploadResponse struct {
	Batch  importBatchResponse   `json:"batch"`
	Upload uploadSessionResponse `json:"upload"`
}

type importBatchJobResponse struct {
	Batch importBatchResponse `json:"batch"`
	JobID uuid.UUID           `json:"jobId"`
}

type importBatchResponse struct {
	ID                   uuid.UUID  `json:"id"`
	CompanyID            uuid.UUID  `json:"companyId"`
	Operation            string     `json:"operation"`
	Status               string     `json:"status"`
	TotalRows            int        `json:"totalRows"`
	ValidRows            int        `json:"validRows"`
	InvalidRows          int        `json:"invalidRows"`
	WarningRows          int        `json:"warningRows"`
	CommittedRows        int        `json:"committedRows"`
	RejectedRows         int        `json:"rejectedRows"`
	ValidationStartedAt  *time.Time `json:"validationStartedAt,omitempty"`
	ValidationFinishedAt *time.Time `json:"validationFinishedAt,omitempty"`
	CommitStartedAt      *time.Time `json:"commitStartedAt,omitempty"`
	CommitFinishedAt     *time.Time `json:"commitFinishedAt,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

type importBatchArtifactResponse struct {
	ID        uuid.UUID `json:"id"`
	Role      string    `json:"role"`
	Filename  string    `json:"filename"`
	Extension string    `json:"extension"`
	SizeBytes int64     `json:"sizeBytes"`
	CreatedAt time.Time `json:"createdAt"`
}

type importBatchArtifactListResponse struct {
	Data []importBatchArtifactResponse `json:"data"`
}

type artifactDownloadURLResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type importTemplateResponse struct {
	ID           uuid.UUID `json:"id"`
	TemplateType string    `json:"templateType"`
	Version      string    `json:"version"`
	FileFormat   string    `json:"fileFormat"`
	Status       string    `json:"status"`
}

type importTemplateListResponse struct {
	Data []importTemplateResponse `json:"data"`
}

func importBatchResponseFromDomain(batch domain.ImportBatch) importBatchResponse {
	return importBatchResponse{
		ID: batch.ID.UUID(), CompanyID: batch.CompanyID.UUID(), Operation: string(batch.Operation), Status: string(batch.Status),
		TotalRows: batch.TotalRows, ValidRows: batch.ValidRows, InvalidRows: batch.InvalidRows, WarningRows: batch.WarningRows,
		CommittedRows: batch.CommittedRows, RejectedRows: batch.RejectedRows, ValidationStartedAt: batch.ValidationStartedAt,
		ValidationFinishedAt: batch.ValidationFinishedAt, CommitStartedAt: batch.CommitStartedAt, CommitFinishedAt: batch.CommitFinishedAt,
		CreatedAt: batch.CreatedAt, UpdatedAt: batch.UpdatedAt,
	}
}

func parseFileBatchID(value uuid.UUID) (domain.BatchID, error) {
	id, err := domain.ParseBatchID(value.String())
	if err != nil {
		return domain.BatchID{}, fmt.Errorf("invalid batch id: %w", err)
	}
	return id, nil
}

func parseFileArtifactID(value uuid.UUID) (domain.ArtifactID, error) {
	if value == uuid.Nil {
		return domain.ArtifactID{}, errors.New("invalid artifact id")
	}
	return domain.ArtifactID(value), nil
}
