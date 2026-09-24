package restapi

import (
	"encoding/json"
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
	var templateID *domain.TemplateID
	if body.TemplateId != nil {
		parsed, parseErr := domain.ParseTemplateID(body.TemplateId.String())
		if parseErr != nil {
			writeError(w, parseErr)
			return
		}
		templateID = &parsed
	}
	var payrollContext *domain.PayrollImportContext
	if body.PayrollContext != nil {
		value := &domain.PayrollImportContext{
			TaxYear: body.PayrollContext.Year, TaxMonth: body.PayrollContext.Month,
			CoverageFrom: body.PayrollContext.CoverageFrom.Time, CoverageTo: body.PayrollContext.CoverageTo.Time,
			RunType: string(body.PayrollContext.RunType),
		}
		if body.PayrollContext.PayDate != nil {
			payDate := body.PayrollContext.PayDate.Time
			value.PayDate = &payDate
		}
		if body.PayrollContext.CorrectionOfRunId != nil {
			correctionID := uuid.UUID(*body.PayrollContext.CorrectionOfRunId)
			value.CorrectionOfRunID = &correctionID
		}
		payrollContext = value
	}
	batch, err := handler.service.CreateBatch(r.Context(), appfilebatch.CreateBatchInput{
		CompanyID:      companyID,
		Operation:      domain.Operation(body.Operation),
		Filename:       body.Filename,
		Extension:      string(body.Extension),
		MIMEType:       body.ContentType,
		ExpectedSize:   body.ExpectedSize,
		TemplateID:     templateID,
		PayrollContext: payrollContext,
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

func (handler *FileBatchHandler) ListImportBatchSheets(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.ListImportBatchSheetsParams) {
	batchID, err := parseFileBatchID(id)
	if err != nil {
		writeError(w, err)
		return
	}
	sheets, err := handler.service.ListValidationSheets(r.Context(), batchID)
	if err != nil {
		writeError(w, err)
		return
	}
	data := make([]validationSheetResponse, 0, len(sheets))
	for _, sheet := range sheets {
		data = append(data, validationSheetResponse{
			Name: sheet.Name, TotalRows: sheet.TotalRows, ValidRows: sheet.ValidRows,
			InvalidRows: sheet.InvalidRows, BlockingRows: sheet.BlockingRows,
		})
	}
	writeJSON(w, http.StatusOK, validationSheetListResponse{Data: data})
}

func (handler *FileBatchHandler) ListImportBatchValidationIssues(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.ListImportBatchValidationIssuesParams) {
	batchID, err := parseFileBatchID(id)
	if err != nil {
		writeError(w, err)
		return
	}
	issues, err := handler.service.ListValidationIssues(r.Context(), batchID)
	if err != nil {
		writeError(w, err)
		return
	}
	data := make([]validationIssueResponse, 0, len(issues))
	for _, issue := range issues {
		data = append(data, validationIssueResponse{
			ID: issue.ID.UUID(), RowID: issue.RowID.UUID(), SheetName: issue.SheetName,
			RowNo: issue.RowNumber, FieldName: issue.FieldName, ErrorCode: issue.ErrorCode,
			Severity: issue.Severity, Description: issue.Description,
			MaskedValue: issue.MaskedValue, CandidateCount: issue.CandidateCount,
		})
	}
	writeJSON(w, http.StatusOK, validationIssueListResponse{Data: data})
}

func (handler *FileBatchHandler) ResolveImportBatchRow(w http.ResponseWriter, r *http.Request, batchValue openapi_types.UUID, rowValue openapi_types.UUID, _ generated.ResolveImportBatchRowParams) {
	batchID, err := parseFileBatchID(batchValue)
	if err != nil {
		writeError(w, err)
		return
	}
	rowID, err := domain.ParseImportRowID(rowValue.String())
	if err != nil {
		writeError(w, err)
		return
	}
	var body generated.ResolveImportBatchRowJSONBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	employmentID := optionalUUID(body.EmploymentId)
	employeeID := body.EmployeeId
	if err := handler.service.ResolveValidationRow(r.Context(), batchID, rowID, &employeeID, employmentID, body.Reason); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func (handler *FileBatchHandler) CreateImportTemplate(w http.ResponseWriter, r *http.Request, _ generated.CreateImportTemplateParams) {
	var body generated.CreateImportTemplateJSONRequestBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	companyID, err := parseCompanyID(uuid.UUID(body.CompanyId))
	if err != nil {
		writeError(w, err)
		return
	}
	configuration, err := json.Marshal(body.Configuration)
	if err != nil {
		writeError(w, err)
		return
	}
	template, err := handler.service.CreateTemplate(r.Context(), appfilebatch.CreateTemplateInput{CompanyID: companyID, TemplateType: body.TemplateType, Version: body.Version, FileFormat: string(body.FileFormat), Configuration: configuration})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, importTemplateResponseFromDomain(template))
}

func (handler *FileBatchHandler) GetImportTemplate(w http.ResponseWriter, r *http.Request, id generated.ImportTemplateId, _ generated.GetImportTemplateParams) {
	templateID, err := domain.ParseTemplateID(id.String())
	if err != nil {
		writeError(w, err)
		return
	}
	template, err := handler.service.GetTemplate(r.Context(), templateID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, importTemplateResponseFromDomain(template))
}

func (handler *FileBatchHandler) RetireImportTemplate(w http.ResponseWriter, r *http.Request, id generated.ImportTemplateId, _ generated.RetireImportTemplateParams) {
	templateID, err := domain.ParseTemplateID(id.String())
	if err != nil {
		writeError(w, err)
		return
	}
	if err := handler.service.RetireTemplate(r.Context(), templateID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	ID                   uuid.UUID               `json:"id"`
	CompanyID            uuid.UUID               `json:"companyId"`
	Operation            string                  `json:"operation"`
	Status               string                  `json:"status"`
	TotalRows            int                     `json:"totalRows"`
	ValidRows            int                     `json:"validRows"`
	InvalidRows          int                     `json:"invalidRows"`
	WarningRows          int                     `json:"warningRows"`
	CommittedRows        int                     `json:"committedRows"`
	RejectedRows         int                     `json:"rejectedRows"`
	ValidationStartedAt  *time.Time              `json:"validationStartedAt,omitempty"`
	ValidationFinishedAt *time.Time              `json:"validationFinishedAt,omitempty"`
	CommitStartedAt      *time.Time              `json:"commitStartedAt,omitempty"`
	CommitFinishedAt     *time.Time              `json:"commitFinishedAt,omitempty"`
	CreatedAt            time.Time               `json:"createdAt"`
	UpdatedAt            time.Time               `json:"updatedAt"`
	PayrollContext       *payrollContextResponse `json:"payrollContext,omitempty"`
	PayrollPeriodID      *uuid.UUID              `json:"payrollPeriodId,omitempty"`
	PayrollRunID         *uuid.UUID              `json:"payrollRunId,omitempty"`
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

type validationSheetResponse struct {
	Name         string `json:"name"`
	TotalRows    int    `json:"totalRows"`
	ValidRows    int    `json:"validRows"`
	InvalidRows  int    `json:"invalidRows"`
	BlockingRows int    `json:"blockingRows"`
}

type validationSheetListResponse struct {
	Data []validationSheetResponse `json:"data"`
}

type validationIssueResponse struct {
	ID             uuid.UUID `json:"id"`
	RowID          uuid.UUID `json:"rowId"`
	SheetName      string    `json:"sheetName"`
	RowNo          int       `json:"rowNo"`
	FieldName      *string   `json:"fieldName"`
	ErrorCode      string    `json:"errorCode"`
	Severity       string    `json:"severity"`
	Description    string    `json:"description"`
	MaskedValue    *string   `json:"maskedValue"`
	CandidateCount int       `json:"candidateCount"`
}

type validationIssueListResponse struct {
	Data []validationIssueResponse `json:"data"`
}

type artifactDownloadURLResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type importTemplateResponse struct {
	ID            uuid.UUID       `json:"id"`
	TemplateType  string          `json:"templateType"`
	Version       string          `json:"version"`
	FileFormat    string          `json:"fileFormat"`
	Status        string          `json:"status"`
	Configuration json.RawMessage `json:"configuration,omitempty"`
}

func importTemplateResponseFromDomain(item domain.ImportTemplate) importTemplateResponse {
	return importTemplateResponse{ID: item.ID.UUID(), TemplateType: item.TemplateType, Version: item.Version, FileFormat: item.FileFormat, Status: item.Status, Configuration: item.Configuration}
}

type importTemplateListResponse struct {
	Data []importTemplateResponse `json:"data"`
}

type payrollContextResponse struct {
	Year              int        `json:"year"`
	Month             int        `json:"month"`
	CoverageFrom      string     `json:"coverageFrom"`
	CoverageTo        string     `json:"coverageTo"`
	PayDate           *string    `json:"payDate,omitempty"`
	RunType           string     `json:"runType"`
	CorrectionOfRunID *uuid.UUID `json:"correctionOfRunId,omitempty"`
}

func importBatchResponseFromDomain(batch domain.ImportBatch) importBatchResponse {
	response := importBatchResponse{
		ID: batch.ID.UUID(), CompanyID: batch.CompanyID.UUID(), Operation: string(batch.Operation), Status: string(batch.Status),
		TotalRows: batch.TotalRows, ValidRows: batch.ValidRows, InvalidRows: batch.InvalidRows, WarningRows: batch.WarningRows,
		CommittedRows: batch.CommittedRows, RejectedRows: batch.RejectedRows, ValidationStartedAt: batch.ValidationStartedAt,
		ValidationFinishedAt: batch.ValidationFinishedAt, CommitStartedAt: batch.CommitStartedAt, CommitFinishedAt: batch.CommitFinishedAt,
		CreatedAt: batch.CreatedAt, UpdatedAt: batch.UpdatedAt,
	}
	if batch.PayrollContext != nil {
		var payDate *string
		if batch.PayrollContext.PayDate != nil {
			value := batch.PayrollContext.PayDate.Format("2006-01-02")
			payDate = &value
		}
		response.PayrollContext = &payrollContextResponse{Year: batch.PayrollContext.TaxYear, Month: batch.PayrollContext.TaxMonth, CoverageFrom: batch.PayrollContext.CoverageFrom.Format("2006-01-02"), CoverageTo: batch.PayrollContext.CoverageTo.Format("2006-01-02"), PayDate: payDate, RunType: batch.PayrollContext.RunType, CorrectionOfRunID: batch.PayrollContext.CorrectionOfRunID}
		response.PayrollPeriodID = batch.PayrollContext.PayrollPeriodID
		response.PayrollRunID = batch.PayrollContext.PayrollRunID
	}
	return response
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

func optionalUUID(value *openapi_types.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	result := uuid.UUID(*value)
	return &result
}
