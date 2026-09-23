package restapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	appemployee "github.com/navyaraksha/imogi/internal/application/employee"
	domain "github.com/navyaraksha/imogi/internal/domain/employee"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	generated "github.com/navyaraksha/imogi/internal/restapi/generated"
)

type Handler struct {
	generated.Unimplemented
	service *appemployee.Service
	clock   clock.Clock
}

var _ generated.ServerInterface = (*Handler)(nil)

func NewHandler(service *appemployee.Service, systemClock clock.Clock) (*Handler, error) {
	if service == nil || systemClock == nil {
		return nil, errors.New("rest handler dependencies are required")
	}
	return &Handler{service: service, clock: systemClock}, nil
}

func (h *Handler) GetAssignment(w http.ResponseWriter, r *http.Request, assignmentID openapi_types.UUID, _ generated.GetAssignmentParams) {
	id, err := parseAssignmentID(assignmentID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	assignment, err := h.service.GetAssignment(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, assignmentResponseFromDomain(assignment, h.clock.Now()))
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	writeError(w, err)
}

func (h *Handler) ListEmployees(w http.ResponseWriter, r *http.Request, params generated.ListEmployeesParams) {
	var filter appemployee.EmployeeListFilter
	filter.Search = params.Search
	if params.Limit != nil {
		filter.Limit = int(*params.Limit)
	}
	if params.Cursor != nil {
		cursor, err := appemployee.DecodeEmployeeCursor(*params.Cursor)
		if err != nil {
			h.writeError(w, err)
			return
		}
		filter.CursorID = &cursor
	}
	if params.EmploymentStatus != nil {
		status := domain.EmploymentStatus(*params.EmploymentStatus)
		filter.EmploymentStatus = &status
	}
	if params.CompanyId != nil {
		companyID, err := parseCompanyID(*params.CompanyId)
		if err != nil {
			h.writeError(w, err)
			return
		}
		filter.CompanyID = &companyID
	}
	employees, nextCursor, err := h.service.ListEmployees(r.Context(), filter)
	if err != nil {
		h.writeError(w, err)
		return
	}
	data := make([]employeeSummaryResponse, 0, len(employees))
	for _, employee := range employees {
		data = append(data, employeeSummaryResponse{
			ID:             employee.ID.UUID(),
			CompanyID:      employee.CompanyID.UUID(),
			EmployeeNumber: employee.EmployeeNumber,
			FullName:       employee.FullName,
			CreatedAt:      employee.CreatedAt,
			UpdatedAt:      employee.UpdatedAt,
		})
	}
	var next *string
	if nextCursor != "" {
		next = &nextCursor
	}
	writeJSON(w, http.StatusOK, employeeListResponse{Data: data, Pagination: paginationResponse{NextCursor: next}})
}

func (h *Handler) CreateEmployee(w http.ResponseWriter, r *http.Request, _ generated.CreateEmployeeParams) {
	var body generated.CreateEmployeeJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	nik, err := domain.ParseNIK(body.Nik)
	if err != nil {
		h.writeError(w, err)
		return
	}
	companyID, err := parseCompanyID(body.CompanyId)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var gender *domain.Gender
	if body.Gender != nil {
		value, err := domain.ParseGender(string(*body.Gender))
		if err != nil {
			h.writeError(w, err)
			return
		}
		gender = &value
	}
	input := appemployee.CreateEmployeeInput{
		CompanyID:      companyID,
		EmployeeNumber: body.EmployeeNumber,
		NIK:            nik,
		FullName:       body.FullName,
		BirthPlace:     body.BirthPlace,
		BirthDate:      datePtr(body.BirthDate),
		Gender:         gender,
		Email:          emailPtr(body.Email),
		Phone:          body.Phone,
		Address:        body.Address,
	}
	employee, err := h.service.CreateEmployee(r.Context(), input)
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/employees/"+employee.ID.String())
	writeJSON(w, http.StatusCreated, employeeResponse(employee))
}

func (h *Handler) GetEmployee(w http.ResponseWriter, r *http.Request, employeeID openapi_types.UUID, _ generated.GetEmployeeParams) {
	id, err := parseEmployeeID(employeeID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	employee, err := h.service.GetEmployee(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, employeeResponse(employee))
}

func (h *Handler) UpdateEmployeePersonalData(w http.ResponseWriter, r *http.Request, employeeID openapi_types.UUID, _ generated.UpdateEmployeePersonalDataParams) {
	id, err := parseEmployeeID(employeeID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	body, fields, err := decodeUpdateBody(r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	input := appemployee.UpdateEmployeeInput{FullName: body.FullName}
	input.BirthPlace = optionalString(fields, "birthPlace", body.BirthPlace)
	input.BirthDate = optionalDate(fields, "birthDate", body.BirthDate)
	if contains(fields, "gender") {
		if body.Gender == nil {
			input.Gender = appemployee.Optional[domain.Gender]{Set: true}
		} else {
			gender, err := domain.ParseGender(string(*body.Gender))
			if err != nil {
				h.writeError(w, err)
				return
			}
			input.Gender = appemployee.Optional[domain.Gender]{Set: true, Value: &gender}
		}
	}
	input.Email = optionalString(fields, "email", stringPtrFromEmail(body.Email))
	input.Phone = optionalString(fields, "phone", body.Phone)
	input.Address = optionalString(fields, "address", body.Address)
	if contains(fields, "fullName") && body.FullName == nil {
		h.writeError(w, errors.New("fullName cannot be null"))
		return
	}
	employee, err := h.service.UpdateEmployeePersonalData(r.Context(), id, input)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, employeeResponse(employee))
}

func (h *Handler) ListEmployeeEmployments(w http.ResponseWriter, r *http.Request, employeeID openapi_types.UUID, _ generated.ListEmployeeEmploymentsParams) {
	id, err := parseEmployeeID(employeeID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	items, err := h.service.ListEmployments(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	data := make([]employmentResponse, 0, len(items))
	for _, item := range items {
		data = append(data, employmentResponseFromDomain(item, h.clock.Now()))
	}
	writeJSON(w, http.StatusOK, employmentListResponse{Data: data})
}

func (h *Handler) StartEmployment(w http.ResponseWriter, r *http.Request, employeeID openapi_types.UUID, _ generated.StartEmploymentParams) {
	id, err := parseEmployeeID(employeeID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var body generated.StartEmploymentJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	companyID, err := parseCompanyID(body.CompanyId)
	if err != nil {
		h.writeError(w, err)
		return
	}
	employmentType, err := domain.ParseEmploymentType(string(body.EmploymentType))
	if err != nil {
		h.writeError(w, err)
		return
	}
	item, err := h.service.StartEmployment(r.Context(), appemployee.StartEmploymentInput{
		EmployeeID: id, CompanyID: companyID, EmploymentType: employmentType, JoinDate: body.JoinDate.Time,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, employmentResponseFromDomain(item, h.clock.Now()))
}

func (h *Handler) RejoinEmployee(w http.ResponseWriter, r *http.Request, employeeID openapi_types.UUID, _ generated.RejoinEmployeeParams) {
	id, err := parseEmployeeID(employeeID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var body generated.RejoinEmployeeJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	companyID, err := parseCompanyID(body.CompanyId)
	if err != nil {
		h.writeError(w, err)
		return
	}
	employmentType, err := domain.ParseEmploymentType(string(body.EmploymentType))
	if err != nil {
		h.writeError(w, err)
		return
	}
	var previousID *domain.EmploymentID
	if body.PreviousEmploymentId != nil {
		parsed, err := parseEmploymentID(*body.PreviousEmploymentId)
		if err != nil {
			h.writeError(w, err)
			return
		}
		previousID = &parsed
	}
	item, err := h.service.RejoinEmployee(r.Context(), appemployee.RejoinEmploymentInput{
		EmployeeID: id, CompanyID: companyID, EmploymentType: employmentType, JoinDate: body.JoinDate.Time, PreviousEmploymentID: previousID,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, employmentResponseFromDomain(item, h.clock.Now()))
}

func (h *Handler) GetEmployeeIdentity(w http.ResponseWriter, r *http.Request, employeeID openapi_types.UUID, _ generated.GetEmployeeIdentityParams) {
	id, err := parseEmployeeID(employeeID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	item, err := h.service.GetEmployeeIdentity(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.EmployeeIdentityDetail{EmployeeId: item.ID.UUID(), Nik: item.NIK.String()})
}

func (h *Handler) ListEmployeeTaxProfiles(w http.ResponseWriter, r *http.Request, employeeID openapi_types.UUID, _ generated.ListEmployeeTaxProfilesParams) {
	id, err := parseEmployeeID(employeeID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	items, err := h.service.ListTaxProfiles(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	data := make([]taxProfileResponse, 0, len(items))
	for _, item := range items {
		data = append(data, taxProfileResponseFromDomain(item, h.clock.Now()))
	}
	writeJSON(w, http.StatusOK, taxProfileListResponse{Data: data})
}

func (h *Handler) CreateEmployeeTaxProfile(w http.ResponseWriter, r *http.Request, employeeID openapi_types.UUID, _ generated.CreateEmployeeTaxProfileParams) {
	id, err := parseEmployeeID(employeeID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var body generated.CreateEmployeeTaxProfileJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	nik, err := domain.ParseNIK(body.Nik)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var npwp *domain.NPWP
	if body.Npwp != nil {
		parsed, err := domain.ParseNPWP(*body.Npwp)
		if err != nil {
			h.writeError(w, err)
			return
		}
		npwp = &parsed
	}
	item, err := h.service.CreateTaxProfile(r.Context(), appemployee.CreateTaxProfileInput{
		EmployeeID: id, NIK: nik, NPWP: npwp, PTKPCode: body.PtkpCode, TaxMethod: body.TaxMethod, EffectiveFrom: body.EffectiveFrom.Time,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, taxProfileResponseFromDomain(item, h.clock.Now()))
}

func (h *Handler) GetEmployment(w http.ResponseWriter, r *http.Request, employmentID openapi_types.UUID, _ generated.GetEmploymentParams) {
	id, err := parseEmploymentID(employmentID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	item, err := h.service.GetEmployment(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, employmentResponseFromDomain(item, h.clock.Now()))
}

func (h *Handler) ListEmploymentAssignments(w http.ResponseWriter, r *http.Request, employmentID openapi_types.UUID, _ generated.ListEmploymentAssignmentsParams) {
	id, err := parseEmploymentID(employmentID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	items, err := h.service.ListAssignments(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	data := make([]assignmentResponse, 0, len(items))
	for _, item := range items {
		data = append(data, assignmentResponseFromDomain(item, h.clock.Now()))
	}
	writeJSON(w, http.StatusOK, assignmentListResponse{Data: data})
}

func (h *Handler) CreateEmploymentAssignment(w http.ResponseWriter, r *http.Request, employmentID openapi_types.UUID, _ generated.CreateEmploymentAssignmentParams) {
	id, err := parseEmploymentID(employmentID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var body generated.CreateEmploymentAssignmentJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	input := appemployee.CreateAssignmentInput{
		EmploymentID: id, LocationID: unitIDPtr(body.LocationId), DepartmentID: unitIDPtr(body.DepartmentId),
		PositionID: unitIDPtr(body.PositionId), GroupID: unitIDPtr(body.GroupId), EffectiveFrom: body.EffectiveFrom.Time,
	}
	item, err := h.service.CreateAssignment(r.Context(), input)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, assignmentResponseFromDomain(item, h.clock.Now()))
}

func (h *Handler) ResignEmployment(w http.ResponseWriter, r *http.Request, employmentID openapi_types.UUID, _ generated.ResignEmploymentParams) {
	id, err := parseEmploymentID(employmentID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var body generated.ResignEmploymentJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	item, err := h.service.ResignEmployment(r.Context(), appemployee.ResignEmploymentInput{
		EmploymentID: id, LastWorkingDate: body.LastWorkingDate.Time, TerminationReason: body.TerminationReason,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, employmentResponseFromDomain(item, h.clock.Now()))
}

func (h *Handler) GetTaxProfile(w http.ResponseWriter, r *http.Request, taxProfileID openapi_types.UUID, _ generated.GetTaxProfileParams) {
	id, err := parseTaxProfileID(taxProfileID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	item, err := h.service.GetTaxProfile(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, taxProfileResponseFromDomain(item, h.clock.Now()))
}

type employeeSummaryResponse struct {
	ID             uuid.UUID `json:"id"`
	CompanyID      uuid.UUID `json:"companyId"`
	EmployeeNumber string    `json:"employeeNumber"`
	FullName       string    `json:"fullName"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type employeeListResponse struct {
	Data       []employeeSummaryResponse `json:"data"`
	Pagination paginationResponse        `json:"pagination"`
}

type paginationResponse struct {
	NextCursor *string `json:"nextCursor"`
}

type employmentListResponse struct {
	Data []employmentResponse `json:"data"`
}

type assignmentListResponse struct {
	Data []assignmentResponse `json:"data"`
}

type taxProfileListResponse struct {
	Data []taxProfileResponse `json:"data"`
}

type employeeResponseDTO struct {
	ID             uuid.UUID           `json:"id"`
	CompanyID      uuid.UUID           `json:"companyId"`
	EmployeeNumber string              `json:"employeeNumber"`
	FullName       string              `json:"fullName"`
	BirthPlace     *string             `json:"birthPlace"`
	BirthDate      *openapi_types.Date `json:"birthDate"`
	Gender         *domain.Gender      `json:"gender"`
	Email          *string             `json:"email"`
	Phone          *string             `json:"phone"`
	Address        *string             `json:"address"`
	CreatedAt      time.Time           `json:"createdAt"`
	UpdatedAt      time.Time           `json:"updatedAt"`
}

type employmentResponse struct {
	ID                uuid.UUID           `json:"id"`
	EmployeeID        uuid.UUID           `json:"employeeId"`
	CompanyID         uuid.UUID           `json:"companyId"`
	EmploymentType    string              `json:"employmentType"`
	JoinDate          openapi_types.Date  `json:"joinDate"`
	EndDate           *openapi_types.Date `json:"endDate"`
	TerminationReason *string             `json:"terminationReason"`
	Status            string              `json:"status"`
	CreatedAt         time.Time           `json:"createdAt"`
	UpdatedAt         time.Time           `json:"updatedAt"`
}

type assignmentResponse struct {
	ID            uuid.UUID           `json:"id"`
	EmploymentID  uuid.UUID           `json:"employmentId"`
	LocationID    *uuid.UUID          `json:"locationId"`
	DepartmentID  *uuid.UUID          `json:"departmentId"`
	PositionID    *uuid.UUID          `json:"positionId"`
	GroupID       *uuid.UUID          `json:"groupId"`
	EffectiveFrom openapi_types.Date  `json:"effectiveFrom"`
	EffectiveTo   *openapi_types.Date `json:"effectiveTo"`
	Status        string              `json:"status"`
	CreatedAt     time.Time           `json:"createdAt"`
	UpdatedAt     time.Time           `json:"updatedAt"`
}

type taxProfileResponse struct {
	ID            uuid.UUID           `json:"id"`
	EmployeeID    uuid.UUID           `json:"employeeId"`
	NIK           string              `json:"nik"`
	NPWP          *string             `json:"npwp"`
	PTKPCode      string              `json:"ptkpCode"`
	TaxMethod     string              `json:"taxMethod"`
	EffectiveFrom openapi_types.Date  `json:"effectiveFrom"`
	EffectiveTo   *openapi_types.Date `json:"effectiveTo"`
	Status        string              `json:"status"`
	CreatedAt     time.Time           `json:"createdAt"`
	UpdatedAt     time.Time           `json:"updatedAt"`
}

func employeeResponse(employee domain.Employee) employeeResponseDTO {
	return employeeResponseDTO{
		ID:             employee.ID.UUID(),
		CompanyID:      employee.CompanyID.UUID(),
		EmployeeNumber: employee.EmployeeNumber,
		FullName:       employee.FullName,
		BirthPlace:     employee.BirthPlace,
		BirthDate:      datePtrFromTime(employee.BirthDate),
		Gender:         employee.Gender,
		Email:          employee.Email,
		Phone:          employee.Phone,
		Address:        employee.Address,
		CreatedAt:      employee.CreatedAt,
		UpdatedAt:      employee.UpdatedAt,
	}
}

func employmentResponseFromDomain(employment domain.Employment, now time.Time) employmentResponse {
	return employmentResponse{
		ID:                employment.ID.UUID(),
		EmployeeID:        employment.EmployeeID.UUID(),
		CompanyID:         employment.CompanyID.UUID(),
		EmploymentType:    string(employment.EmploymentType),
		JoinDate:          openDate(employment.JoinDate),
		EndDate:           datePtrFromTime(employment.EndDate),
		TerminationReason: employment.TerminationReason,
		Status:            string(employment.Status(now)),
		CreatedAt:         employment.CreatedAt,
		UpdatedAt:         employment.UpdatedAt,
	}
}

func assignmentResponseFromDomain(assignment domain.Assignment, now time.Time) assignmentResponse {
	return assignmentResponse{
		ID:            assignment.ID.UUID(),
		EmploymentID:  assignment.EmploymentID.UUID(),
		LocationID:    unitUUIDPtr(assignment.LocationID),
		DepartmentID:  unitUUIDPtr(assignment.DepartmentID),
		PositionID:    unitUUIDPtr(assignment.PositionID),
		GroupID:       unitUUIDPtr(assignment.GroupID),
		EffectiveFrom: openDate(assignment.EffectiveFrom),
		EffectiveTo:   datePtrFromTime(assignment.EffectiveTo),
		Status:        string(assignment.Status(now)),
		CreatedAt:     assignment.CreatedAt,
		UpdatedAt:     assignment.UpdatedAt,
	}
}

func taxProfileResponseFromDomain(profile domain.TaxProfile, now time.Time) taxProfileResponse {
	var npwp *string
	if profile.NPWP != nil {
		npwpValue := profile.NPWP.String()
		npwp = &npwpValue
	}
	return taxProfileResponse{
		ID:            profile.ID.UUID(),
		EmployeeID:    profile.EmployeeID.UUID(),
		NIK:           profile.NIK.String(),
		NPWP:          npwp,
		PTKPCode:      profile.PTKPCode,
		TaxMethod:     profile.TaxMethod,
		EffectiveFrom: openDate(profile.EffectiveFrom),
		EffectiveTo:   datePtrFromTime(profile.EffectiveTo),
		Status:        string(profile.Status(now)),
		CreatedAt:     profile.CreatedAt,
		UpdatedAt:     profile.UpdatedAt,
	}
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("request body is required")
		}
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

func decodeUpdateBody(r *http.Request) (generated.UpdateEmployeePersonalDataJSONBody, map[string]json.RawMessage, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return generated.UpdateEmployeePersonalDataJSONBody{}, nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return generated.UpdateEmployeePersonalDataJSONBody{}, nil, errors.New("request body is required")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return generated.UpdateEmployeePersonalDataJSONBody{}, nil, err
	}
	if fields == nil {
		return generated.UpdateEmployeePersonalDataJSONBody{}, nil, errors.New("request body must be a JSON object")
	}
	var body generated.UpdateEmployeePersonalDataJSONBody
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return generated.UpdateEmployeePersonalDataJSONBody{}, nil, err
	}
	return body, fields, nil
}

func optionalString(fields map[string]json.RawMessage, name string, value *string) appemployee.Optional[string] {
	if !contains(fields, name) {
		return appemployee.Optional[string]{}
	}
	return appemployee.Optional[string]{Set: true, Value: value}
}

func optionalDate(fields map[string]json.RawMessage, name string, value *openapi_types.Date) appemployee.Optional[time.Time] {
	if !contains(fields, name) {
		return appemployee.Optional[time.Time]{}
	}
	if value == nil {
		return appemployee.Optional[time.Time]{Set: true}
	}
	date := value.Time
	return appemployee.Optional[time.Time]{Set: true, Value: &date}
}

func contains(fields map[string]json.RawMessage, key string) bool {
	_, ok := fields[key]
	return ok
}

func emailPtr(value *openapi_types.Email) *string {
	if value == nil {
		return nil
	}
	result := string(*value)
	return &result
}

func stringPtrFromEmail(value *openapi_types.Email) *string { return emailPtr(value) }

func datePtr(value *openapi_types.Date) *time.Time {
	if value == nil {
		return nil
	}
	result := value.Time
	return &result
}

func datePtrFromTime(value *time.Time) *openapi_types.Date {
	if value == nil {
		return nil
	}
	return &openapi_types.Date{Time: *value}
}

func openDate(value time.Time) openapi_types.Date { return openapi_types.Date{Time: value} }

func unitIDPtr(value *openapi_types.UUID) *organization.UnitID {
	if value == nil {
		return nil
	}
	result := organization.UnitID(*value)
	return &result
}

func unitUUIDPtr(value *organization.UnitID) *uuid.UUID {
	if value == nil {
		return nil
	}
	result := value.UUID()
	return &result
}

func parseEmployeeID(value uuid.UUID) (domain.EmployeeID, error) {
	result, err := domain.ParseEmployeeID(value.String())
	if err != nil {
		return domain.EmployeeID{}, fmt.Errorf("%w: %v", domain.ErrInvalidEmployee, err)
	}
	return result, nil
}
func parseEmploymentID(value uuid.UUID) (domain.EmploymentID, error) {
	result, err := domain.ParseEmploymentID(value.String())
	if err != nil {
		return domain.EmploymentID{}, fmt.Errorf("%w: %v", domain.ErrInvalidEmployment, err)
	}
	return result, nil
}
func parseAssignmentID(value uuid.UUID) (domain.AssignmentID, error) {
	result, err := domain.ParseAssignmentID(value.String())
	if err != nil {
		return domain.AssignmentID{}, fmt.Errorf("%w: %v", domain.ErrInvalidAssignment, err)
	}
	return result, nil
}
func parseTaxProfileID(value uuid.UUID) (domain.TaxProfileID, error) {
	result, err := domain.ParseTaxProfileID(value.String())
	if err != nil {
		return domain.TaxProfileID{}, fmt.Errorf("%w: %v", domain.ErrInvalidTaxProfile, err)
	}
	return result, nil
}
func parseCompanyID(value uuid.UUID) (organization.CompanyID, error) {
	result, err := organization.ParseCompanyID(value.String())
	if err != nil {
		return organization.CompanyID{}, fmt.Errorf("%w: %v", domain.ErrInvalidEmployment, err)
	}
	return result, nil
}
func parseTenantID(value uuid.UUID) (organization.TenantID, error) {
	result, err := organization.ParseTenantID(value.String())
	if err != nil {
		return organization.TenantID{}, fmt.Errorf("invalid tenant id: %w", err)
	}
	return result, nil
}
func parseUnitID(value uuid.UUID) (organization.UnitID, error) {
	result, err := organization.ParseUnitID(value.String())
	if err != nil {
		return organization.UnitID{}, fmt.Errorf("invalid organization unit id: %w", err)
	}
	return result, nil
}
