package restapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	apppayroll "github.com/navyaraksha/imogi/internal/application/payroll"
	domain "github.com/navyaraksha/imogi/internal/domain/payroll"
	generated "github.com/navyaraksha/imogi/internal/restapi/generated"
)

// PayrollHandler is the HTTP adapter for the payroll ledger. It only maps
// transport DTOs to application inputs and domain results to API responses.
type PayrollHandler struct {
	generated.Unimplemented
	service *apppayroll.Service
}

var _ generated.ServerInterface = (*PayrollHandler)(nil)

func NewPayrollHandler(service *apppayroll.Service) (*PayrollHandler, error) {
	if service == nil {
		return nil, errors.New("payroll service is required")
	}
	return &PayrollHandler{service: service}, nil
}

func (h *PayrollHandler) writeError(w http.ResponseWriter, err error) {
	writeError(w, err)
}

func (h *PayrollHandler) ListPayrollPeriods(w http.ResponseWriter, r *http.Request, params generated.ListPayrollPeriodsParams) {
	companyID, err := parseCompanyID(params.CompanyId)
	if err != nil {
		h.writeError(w, err)
		return
	}
	filter := apppayroll.PeriodFilter{CompanyID: companyID, Limit: intValue(params.Limit)}
	if params.Status != nil {
		status := domain.PeriodStatus(*params.Status)
		filter.Status = &status
	}
	if params.Cursor != nil {
		cursor, cursorErr := apppayroll.DecodePeriodCursor(*params.Cursor)
		if cursorErr != nil {
			h.writeError(w, cursorErr)
			return
		}
		filter.CursorID = &cursor
	}
	periods, nextCursor, err := h.service.ListPayrollPeriods(r.Context(), filter)
	if err != nil {
		h.writeError(w, err)
		return
	}
	data := make([]payrollPeriodResponse, 0, len(periods))
	for _, period := range periods {
		data = append(data, payrollPeriodResponseFromDomain(period))
	}
	writeJSON(w, http.StatusOK, payrollPeriodListResponse{Data: data, Pagination: paginationResponse{NextCursor: stringPtr(nextCursor)}})
}

func (h *PayrollHandler) CreatePayrollPeriod(w http.ResponseWriter, r *http.Request, _ generated.CreatePayrollPeriodParams) {
	var body generated.CreatePayrollPeriodJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	companyID, err := parseCompanyID(body.CompanyId)
	if err != nil {
		h.writeError(w, err)
		return
	}
	period, err := h.service.CreatePayrollPeriod(r.Context(), apppayroll.CreatePayrollPeriodInput{
		CompanyID: companyID,
		Year:      body.Year,
		Month:     body.Month,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/payroll-periods/"+period.ID.String())
	writeJSON(w, http.StatusCreated, payrollPeriodResponseFromDomain(period))
}

func (h *PayrollHandler) GetPayrollPeriod(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.GetPayrollPeriodParams) {
	periodID, err := parsePayrollPeriodID(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	period, err := h.service.GetPayrollPeriod(r.Context(), periodID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payrollPeriodResponseFromDomain(period))
}

func (h *PayrollHandler) RecordPayrollResult(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.RecordPayrollResultParams) {
	periodID, err := parsePayrollPeriodID(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var body generated.RecordPayrollResultJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	employeeID, err := parseEmployeeID(body.EmployeeId)
	if err != nil {
		h.writeError(w, err)
		return
	}
	employmentID, err := parseEmploymentID(body.EmploymentId)
	if err != nil {
		h.writeError(w, err)
		return
	}
	items := make([]apppayroll.CreatePayrollResultItemInput, 0, len(body.Items))
	for _, item := range body.Items {
		items = append(items, apppayroll.CreatePayrollResultItemInput{
			ComponentCode: item.ComponentCode,
			ComponentType: domain.ComponentType(item.ComponentType),
			Amount:        item.Amount,
		})
	}
	history, err := h.service.RecordPayrollResult(r.Context(), apppayroll.CreatePayrollResultInput{
		PayrollPeriodID: periodID,
		EmployeeID:      employeeID,
		EmploymentID:    employmentID,
		GrossIncome:     body.GrossIncome,
		TaxableIncome:   body.TaxableIncome,
		TakeHomePay:     body.TakeHomePay,
		Items:           items,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/payroll-periods/"+periodID.String()+"/results/"+history.Result.ID.String())
	writeJSON(w, http.StatusCreated, payrollHistoryEntryResponseFromDomain(history))
}

func (h *PayrollHandler) FinalizePayrollPeriod(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.FinalizePayrollPeriodParams) {
	periodID, err := parsePayrollPeriodID(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	period, err := h.service.FinalizePayrollPeriod(r.Context(), periodID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payrollPeriodResponseFromDomain(period))
}

func (h *PayrollHandler) ListEmployeePayrollHistory(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, params generated.ListEmployeePayrollHistoryParams) {
	employeeID, err := parseEmployeeID(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	filter := apppayroll.HistoryFilter{Limit: intValue(params.Limit)}
	if params.Year != nil {
		filter.Year = params.Year
	}
	if params.Cursor != nil {
		cursor, cursorErr := apppayroll.DecodeResultCursor(*params.Cursor)
		if cursorErr != nil {
			h.writeError(w, cursorErr)
			return
		}
		filter.CursorID = &cursor
	}
	history, nextCursor, err := h.service.ListPayrollHistory(r.Context(), employeeID, filter)
	if err != nil {
		h.writeError(w, err)
		return
	}
	data := make([]payrollHistoryEntryResponse, 0, len(history))
	for _, entry := range history {
		data = append(data, payrollHistoryEntryResponseFromDomain(entry))
	}
	writeJSON(w, http.StatusOK, payrollHistoryResponse{Data: data, Pagination: paginationResponse{NextCursor: stringPtr(nextCursor)}})
}

func parsePayrollPeriodID(value uuid.UUID) (domain.PayrollPeriodID, error) {
	id, err := domain.ParsePayrollPeriodID(value.String())
	if err != nil {
		return domain.PayrollPeriodID{}, fmt.Errorf("%w: %v", domain.ErrInvalidPayrollPeriod, err)
	}
	return id, nil
}

type payrollPeriodResponse struct {
	ID          uuid.UUID  `json:"id"`
	CompanyID   uuid.UUID  `json:"companyId"`
	Year        int        `json:"year"`
	Month       int        `json:"month"`
	Status      string     `json:"status"`
	OpenedAt    time.Time  `json:"openedAt"`
	FinalizedAt *time.Time `json:"finalizedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type payrollResultResponse struct {
	ID              uuid.UUID             `json:"id"`
	PayrollPeriodID uuid.UUID             `json:"payrollPeriodId"`
	EmployeeID      uuid.UUID             `json:"employeeId"`
	EmploymentID    uuid.UUID             `json:"employmentId"`
	GrossIncome     int64                 `json:"grossIncome"`
	TaxableIncome   int64                 `json:"taxableIncome"`
	TakeHomePay     int64                 `json:"takeHomePay"`
	FinalizedAt     *time.Time            `json:"finalizedAt"`
	CreatedAt       time.Time             `json:"createdAt"`
	UpdatedAt       time.Time             `json:"updatedAt"`
	Items           []payrollItemResponse `json:"items"`
}

type payrollItemResponse struct {
	ID              uuid.UUID `json:"id"`
	PayrollResultID uuid.UUID `json:"payrollResultId"`
	ComponentCode   string    `json:"componentCode"`
	ComponentType   string    `json:"componentType"`
	Amount          int64     `json:"amount"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type payrollHistoryEntryResponse struct {
	Period payrollPeriodResponse `json:"period"`
	Result payrollResultResponse `json:"result"`
}

type payrollPeriodListResponse struct {
	Data       []payrollPeriodResponse `json:"data"`
	Pagination paginationResponse      `json:"pagination"`
}

type payrollHistoryResponse struct {
	Data       []payrollHistoryEntryResponse `json:"data"`
	Pagination paginationResponse            `json:"pagination"`
}

func payrollPeriodResponseFromDomain(period domain.PayrollPeriod) payrollPeriodResponse {
	return payrollPeriodResponse{
		ID:          period.ID.UUID(),
		CompanyID:   period.CompanyID.UUID(),
		Year:        period.Year,
		Month:       period.Month,
		Status:      string(period.Status),
		OpenedAt:    period.OpenedAt,
		FinalizedAt: period.FinalizedAt,
		CreatedAt:   period.CreatedAt,
		UpdatedAt:   period.UpdatedAt,
	}
}

func payrollResultResponseFromDomain(result domain.PayrollResult, items []domain.PayrollResultItem) payrollResultResponse {
	response := payrollResultResponse{
		ID:              result.ID.UUID(),
		PayrollPeriodID: result.PayrollPeriodID.UUID(),
		EmployeeID:      result.EmployeeID.UUID(),
		EmploymentID:    result.EmploymentID.UUID(),
		GrossIncome:     result.GrossIncome.Int64(),
		TaxableIncome:   result.TaxableIncome.Int64(),
		TakeHomePay:     result.TakeHomePay.Int64(),
		FinalizedAt:     result.FinalizedAt,
		CreatedAt:       result.CreatedAt,
		UpdatedAt:       result.UpdatedAt,
		Items:           make([]payrollItemResponse, 0, len(items)),
	}
	for _, item := range items {
		response.Items = append(response.Items, payrollItemResponse{
			ID:              item.ID.UUID(),
			PayrollResultID: item.PayrollResultID.UUID(),
			ComponentCode:   item.ComponentCode,
			ComponentType:   string(item.ComponentType),
			Amount:          item.Amount.Int64(),
			CreatedAt:       item.CreatedAt,
			UpdatedAt:       item.UpdatedAt,
		})
	}
	return response
}

func payrollHistoryEntryResponseFromDomain(entry domain.PayrollHistoryEntry) payrollHistoryEntryResponse {
	return payrollHistoryEntryResponse{
		Period: payrollPeriodResponseFromDomain(entry.Period),
		Result: payrollResultResponseFromDomain(entry.Result, entry.Items),
	}
}
