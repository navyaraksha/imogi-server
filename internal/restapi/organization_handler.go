package restapi

import (
	"errors"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	appemployee "github.com/navyaraksha/imogi/internal/application/employee"
	appfilebatch "github.com/navyaraksha/imogi/internal/application/filebatch"
	appidentity "github.com/navyaraksha/imogi/internal/application/identity"
	apporganization "github.com/navyaraksha/imogi/internal/application/organization"
	apppayroll "github.com/navyaraksha/imogi/internal/application/payroll"
	domain "github.com/navyaraksha/imogi/internal/domain/organization"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	generated "github.com/navyaraksha/imogi/internal/restapi/generated"
)

// OrganizationHandler owns organization and tenant transport concerns. It is
// composed with the employee handler by APIHandler so generated OpenAPI
// adapters remain split along module boundaries.
type OrganizationHandler struct {
	generated.Unimplemented
	service *apporganization.Service
}

func (h *OrganizationHandler) writeError(w http.ResponseWriter, err error) {
	writeError(w, err)
}

func NewOrganizationHandler(service *apporganization.Service) (*OrganizationHandler, error) {
	if service == nil {
		return nil, errors.New("organization service is required")
	}
	return &OrganizationHandler{service: service}, nil
}

type APIHandler struct {
	*Handler
	*OrganizationHandler
	*IdentityHandler
	*AuthHandler
	*PayrollHandler
	*FileBatchHandler
}

var _ generated.ServerInterface = (*APIHandler)(nil)

func NewAPIHandler(employee *Handler, organization *OrganizationHandler, identity *IdentityHandler, auth *AuthHandler, payroll *PayrollHandler, fileBatch *FileBatchHandler) (*APIHandler, error) {
	if employee == nil || organization == nil || identity == nil || auth == nil || payroll == nil || fileBatch == nil {
		return nil, errors.New("employee, organization, identity, auth, payroll, and file batch handlers are required")
	}
	return &APIHandler{Handler: employee, OrganizationHandler: organization, IdentityHandler: identity, AuthHandler: auth, PayrollHandler: payroll, FileBatchHandler: fileBatch}, nil
}

// NewHandlerWithOrganization is the composition root for the current REST
// surface. It keeps employee and organization handlers separate while exposing
// one generated OpenAPI server implementation to the router.
func NewHandlerWithOrganization(employeeService *appemployee.Service, organizationService *apporganization.Service, identityService *appidentity.ProvisioningService, sessionService *appidentity.SessionService, payrollService *apppayroll.Service, fileBatchService *appfilebatch.Service, sessionCookieSecure bool, systemClock clock.Clock) (*APIHandler, error) {
	employeeHandler, err := NewHandler(employeeService, systemClock)
	if err != nil {
		return nil, err
	}
	organizationHandler, err := NewOrganizationHandler(organizationService)
	if err != nil {
		return nil, err
	}
	identityHandler, err := NewIdentityHandler(identityService)
	if err != nil {
		return nil, err
	}
	authHandler, err := NewAuthHandlerWithSession(sessionService, sessionCookieSecure)
	if err != nil {
		return nil, err
	}
	payrollHandler, err := NewPayrollHandler(payrollService)
	if err != nil {
		return nil, err
	}
	fileBatchHandler, err := NewFileBatchHandler(fileBatchService)
	if err != nil {
		return nil, err
	}
	return NewAPIHandler(employeeHandler, organizationHandler, identityHandler, authHandler, payrollHandler, fileBatchHandler)
}

func (h *OrganizationHandler) ListPlatformTenants(w http.ResponseWriter, r *http.Request, params generated.ListPlatformTenantsParams) {
	tenants, next, err := h.service.ListTenants(r.Context(), stringValue(params.Cursor), intValue(params.Limit))
	if err != nil {
		h.writeError(w, err)
		return
	}
	data := make([]tenantResponse, 0, len(tenants))
	for _, tenant := range tenants {
		data = append(data, tenantResponseFromDomain(tenant))
	}
	writeJSON(w, http.StatusOK, listResponse[tenantResponse]{Data: data, Pagination: paginationResponse{NextCursor: stringPtr(next)}})
}

func (h *OrganizationHandler) CreatePlatformTenant(w http.ResponseWriter, r *http.Request) {
	var body generated.CreatePlatformTenantJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	tenant, err := h.service.CreateTenant(r.Context(), apporganization.CreateTenantInput{
		Slug: body.Slug,
		Name: body.Name,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/platform/tenants/"+tenant.ID.String())
	writeJSON(w, http.StatusCreated, tenantResponseFromDomain(tenant))
}

func (h *OrganizationHandler) GetPlatformTenant(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	tenantID, err := parseTenantID(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	tenant, err := h.service.GetTenant(r.Context(), tenantID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantResponseFromDomain(tenant))
}

func (h *OrganizationHandler) UpdatePlatformTenant(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	tenantID, err := parseTenantID(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var body generated.UpdatePlatformTenantJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	if body.Name == nil {
		h.writeError(w, errors.New("name is required"))
		return
	}
	tenant, err := h.service.UpdateTenant(r.Context(), tenantID, apporganization.UpdateTenantInput{Name: *body.Name})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantResponseFromDomain(tenant))
}

func (h *OrganizationHandler) ActivatePlatformTenant(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	h.transitionTenant(w, r, id, domain.TenantActive)
}
func (h *OrganizationHandler) SuspendPlatformTenant(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	h.transitionTenant(w, r, id, domain.TenantSuspended)
}
func (h *OrganizationHandler) ArchivePlatformTenant(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	h.transitionTenant(w, r, id, domain.TenantArchived)
}
func (h *OrganizationHandler) transitionTenant(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, status domain.TenantStatus) {
	tenantID, err := parseTenantID(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	item, err := h.service.TransitionTenant(r.Context(), tenantID, status)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantResponseFromDomain(item))
}

func (h *OrganizationHandler) GetCurrentTenant(w http.ResponseWriter, r *http.Request, _ generated.GetCurrentTenantParams) {
	item, err := h.service.GetCurrentTenant(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantResponseFromDomain(item))
}
func (h *OrganizationHandler) UpdateCurrentTenant(w http.ResponseWriter, r *http.Request, _ generated.UpdateCurrentTenantParams) {
	var body generated.UpdateCurrentTenantJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	if body.Name == nil {
		h.writeError(w, errors.New("name is required"))
		return
	}
	item, err := h.service.UpdateCurrentTenant(r.Context(), apporganization.UpdateTenantInput{Name: *body.Name})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantResponseFromDomain(item))
}

func (h *OrganizationHandler) ListCompanies(w http.ResponseWriter, r *http.Request, params generated.ListCompaniesParams) {
	companies, next, err := h.service.ListCompanies(r.Context(), stringValue(params.Cursor), intValue(params.Limit))
	if err != nil {
		h.writeError(w, err)
		return
	}
	data := make([]companyResponse, 0, len(companies))
	for _, company := range companies {
		data = append(data, companyResponseFromDomain(company))
	}
	writeJSON(w, http.StatusOK, listResponse[companyResponse]{Data: data, Pagination: paginationResponse{NextCursor: stringPtr(next)}})
}
func (h *OrganizationHandler) CreateCompany(w http.ResponseWriter, r *http.Request, _ generated.CreateCompanyParams) {
	var body generated.CreateCompanyJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	company, err := h.service.CreateCompany(r.Context(), apporganization.CreateCompanyInput{
		Code:        body.Code,
		LegalName:   body.LegalName,
		DisplayName: body.DisplayName,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/companies/"+company.ID.String())
	writeJSON(w, http.StatusCreated, companyResponseFromDomain(company))
}
func (h *OrganizationHandler) GetCompany(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.GetCompanyParams) {
	companyID, err := parseCompanyID(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	company, err := h.service.GetCompany(r.Context(), companyID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, companyResponseFromDomain(company))
}
func (h *OrganizationHandler) UpdateCompany(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.UpdateCompanyParams) {
	companyID, err := parseCompanyID(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var body generated.UpdateCompanyJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	current, err := h.service.GetCompany(r.Context(), companyID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if body.Code != nil {
		current.Code = *body.Code
	}
	if body.LegalName != nil {
		current.LegalName = *body.LegalName
	}
	if body.DisplayName != nil {
		current.DisplayName = *body.DisplayName
	}
	company, err := h.service.UpdateCompany(r.Context(), companyID, apporganization.UpdateCompanyInput{
		Code:        current.Code,
		LegalName:   current.LegalName,
		DisplayName: current.DisplayName,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, companyResponseFromDomain(company))
}
func (h *OrganizationHandler) ActivateCompany(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.ActivateCompanyParams) {
	h.transitionCompany(w, r, id, domain.CompanyActive)
}
func (h *OrganizationHandler) SuspendCompany(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.SuspendCompanyParams) {
	h.transitionCompany(w, r, id, domain.CompanySuspended)
}
func (h *OrganizationHandler) ArchiveCompany(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.ArchiveCompanyParams) {
	h.transitionCompany(w, r, id, domain.CompanyArchived)
}
func (h *OrganizationHandler) transitionCompany(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, status domain.CompanyStatus) {
	companyID, err := parseCompanyID(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	company, err := h.service.TransitionCompany(r.Context(), companyID, status)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, companyResponseFromDomain(company))
}

func (h *OrganizationHandler) listUnits(w http.ResponseWriter, r *http.Request, unitType domain.UnitType, cursor string, companyID *openapi_types.UUID, limit int) {
	var company *domain.CompanyID
	if companyID != nil {
		parsed, err := parseCompanyID(*companyID)
		if err != nil {
			h.writeError(w, err)
			return
		}
		company = &parsed
	}
	units, next, err := h.service.ListUnits(r.Context(), unitType, cursor, company, limit)
	if err != nil {
		h.writeError(w, err)
		return
	}
	data := make([]unitResponse, 0, len(units))
	for _, unit := range units {
		data = append(data, unitResponseFromDomain(unit))
	}
	writeJSON(w, http.StatusOK, listResponse[unitResponse]{Data: data, Pagination: paginationResponse{NextCursor: stringPtr(next)}})
}
func (h *OrganizationHandler) createUnit(w http.ResponseWriter, r *http.Request, unitType domain.UnitType, companyID openapi_types.UUID, code, name string) {
	parsed, err := parseCompanyID(companyID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	item, err := h.service.CreateUnit(r.Context(), apporganization.CreateUnitInput{CompanyID: parsed, Type: unitType, Code: code, Name: name})
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/organization-units/"+item.ID.String())
	writeJSON(w, http.StatusCreated, unitResponseFromDomain(item))
}
func (h *OrganizationHandler) getUnit(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, unitType domain.UnitType) {
	unitID, err := parseUnitID(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	item, err := h.service.GetUnit(r.Context(), unitID, unitType)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, unitResponseFromDomain(item))
}
func (h *OrganizationHandler) updateUnit(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, unitType domain.UnitType, code, name *string) {
	unitID, err := parseUnitID(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	current, err := h.service.GetUnit(r.Context(), unitID, unitType)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if code != nil {
		current.Code = *code
	}
	if name != nil {
		current.Name = *name
	}
	item, err := h.service.UpdateUnit(r.Context(), unitID, unitType, apporganization.UpdateUnitInput{Code: current.Code, Name: current.Name})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, unitResponseFromDomain(item))
}
func (h *OrganizationHandler) archiveUnit(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, unitType domain.UnitType) {
	unitID, err := parseUnitID(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	item, err := h.service.ArchiveUnit(r.Context(), unitID, unitType)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, unitResponseFromDomain(item))
}

func (h *OrganizationHandler) ListLocations(w http.ResponseWriter, r *http.Request, p generated.ListLocationsParams) {
	h.listUnits(w, r, domain.UnitLocation, stringValue(p.Cursor), p.CompanyId, intValue(p.Limit))
}
func (h *OrganizationHandler) CreateLocation(w http.ResponseWriter, r *http.Request, _ generated.CreateLocationParams) {
	var body generated.CreateLocationJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	h.createUnit(w, r, domain.UnitLocation, body.CompanyId, body.Code, body.Name)
}
func (h *OrganizationHandler) GetLocation(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.GetLocationParams) {
	h.getUnit(w, r, id, domain.UnitLocation)
}
func (h *OrganizationHandler) UpdateLocation(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.UpdateLocationParams) {
	var body generated.UpdateLocationJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	h.updateUnit(w, r, id, domain.UnitLocation, body.Code, body.Name)
}
func (h *OrganizationHandler) ArchiveLocation(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.ArchiveLocationParams) {
	h.archiveUnit(w, r, id, domain.UnitLocation)
}
func (h *OrganizationHandler) ListDepartments(w http.ResponseWriter, r *http.Request, p generated.ListDepartmentsParams) {
	h.listUnits(w, r, domain.UnitDepartment, stringValue(p.Cursor), p.CompanyId, intValue(p.Limit))
}
func (h *OrganizationHandler) CreateDepartment(w http.ResponseWriter, r *http.Request, _ generated.CreateDepartmentParams) {
	var body generated.CreateDepartmentJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	h.createUnit(w, r, domain.UnitDepartment, body.CompanyId, body.Code, body.Name)
}
func (h *OrganizationHandler) GetDepartment(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.GetDepartmentParams) {
	h.getUnit(w, r, id, domain.UnitDepartment)
}
func (h *OrganizationHandler) UpdateDepartment(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.UpdateDepartmentParams) {
	var body generated.UpdateDepartmentJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	h.updateUnit(w, r, id, domain.UnitDepartment, body.Code, body.Name)
}
func (h *OrganizationHandler) ArchiveDepartment(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.ArchiveDepartmentParams) {
	h.archiveUnit(w, r, id, domain.UnitDepartment)
}
func (h *OrganizationHandler) ListPositions(w http.ResponseWriter, r *http.Request, p generated.ListPositionsParams) {
	h.listUnits(w, r, domain.UnitPosition, stringValue(p.Cursor), p.CompanyId, intValue(p.Limit))
}
func (h *OrganizationHandler) CreatePosition(w http.ResponseWriter, r *http.Request, _ generated.CreatePositionParams) {
	var body generated.CreatePositionJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	h.createUnit(w, r, domain.UnitPosition, body.CompanyId, body.Code, body.Name)
}
func (h *OrganizationHandler) GetPosition(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.GetPositionParams) {
	h.getUnit(w, r, id, domain.UnitPosition)
}
func (h *OrganizationHandler) UpdatePosition(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.UpdatePositionParams) {
	var body generated.UpdatePositionJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	h.updateUnit(w, r, id, domain.UnitPosition, body.Code, body.Name)
}
func (h *OrganizationHandler) ArchivePosition(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.ArchivePositionParams) {
	h.archiveUnit(w, r, id, domain.UnitPosition)
}
func (h *OrganizationHandler) ListGroups(w http.ResponseWriter, r *http.Request, p generated.ListGroupsParams) {
	h.listUnits(w, r, domain.UnitGroup, stringValue(p.Cursor), p.CompanyId, intValue(p.Limit))
}
func (h *OrganizationHandler) CreateGroup(w http.ResponseWriter, r *http.Request, _ generated.CreateGroupParams) {
	var body generated.CreateGroupJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	h.createUnit(w, r, domain.UnitGroup, body.CompanyId, body.Code, body.Name)
}
func (h *OrganizationHandler) GetGroup(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.GetGroupParams) {
	h.getUnit(w, r, id, domain.UnitGroup)
}
func (h *OrganizationHandler) UpdateGroup(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.UpdateGroupParams) {
	var body generated.UpdateGroupJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	h.updateUnit(w, r, id, domain.UnitGroup, body.Code, body.Name)
}
func (h *OrganizationHandler) ArchiveGroup(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.ArchiveGroupParams) {
	h.archiveUnit(w, r, id, domain.UnitGroup)
}

type listResponse[T any] struct {
	Data       []T                `json:"data"`
	Pagination paginationResponse `json:"pagination"`
}
type tenantResponse struct {
	ID        openapi_types.UUID `json:"id"`
	Slug      string             `json:"slug"`
	Name      string             `json:"name"`
	Status    string             `json:"status"`
	CreatedAt time.Time          `json:"createdAt"`
	UpdatedAt time.Time          `json:"updatedAt"`
}
type companyResponse struct {
	ID          openapi_types.UUID `json:"id"`
	TenantID    openapi_types.UUID `json:"tenantId"`
	Code        string             `json:"code"`
	LegalName   string             `json:"legalName"`
	DisplayName string             `json:"displayName"`
	Status      string             `json:"status"`
	CreatedAt   time.Time          `json:"createdAt"`
	UpdatedAt   time.Time          `json:"updatedAt"`
}
type unitResponse struct {
	ID        openapi_types.UUID `json:"id"`
	CompanyID openapi_types.UUID `json:"companyId"`
	Code      string             `json:"code"`
	Name      string             `json:"name"`
	Status    string             `json:"status"`
	CreatedAt time.Time          `json:"createdAt"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

func tenantResponseFromDomain(tenant domain.Tenant) tenantResponse {
	return tenantResponse{
		ID:        tenant.ID.UUID(),
		Slug:      tenant.Slug,
		Name:      tenant.Name,
		Status:    string(tenant.Status),
		CreatedAt: tenant.CreatedAt,
		UpdatedAt: tenant.UpdatedAt,
	}
}
func companyResponseFromDomain(company domain.Company) companyResponse {
	return companyResponse{
		ID:          company.ID.UUID(),
		TenantID:    company.TenantID.UUID(),
		Code:        company.Code,
		LegalName:   company.LegalName,
		DisplayName: company.DisplayName,
		Status:      string(company.Status),
		CreatedAt:   company.CreatedAt,
		UpdatedAt:   company.UpdatedAt,
	}
}
func unitResponseFromDomain(unit domain.Unit) unitResponse {
	return unitResponse{
		ID:        unit.ID.UUID(),
		CompanyID: unit.CompanyID.UUID(),
		Code:      unit.Code,
		Name:      unit.Name,
		Status:    string(unit.Status),
		CreatedAt: unit.CreatedAt,
		UpdatedAt: unit.UpdatedAt,
	}
}
func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func intValue(v *int32) int {
	if v == nil {
		return 0
	}
	return int(*v)
}
func stringPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
