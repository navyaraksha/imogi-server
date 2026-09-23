package restapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	appidentity "github.com/navyaraksha/imogi/internal/application/identity"
	domain "github.com/navyaraksha/imogi/internal/domain/identity"
	"github.com/navyaraksha/imogi/internal/platform/pagination"
	generated "github.com/navyaraksha/imogi/internal/restapi/generated"
)

type IdentityHandler struct {
	generated.Unimplemented
	service *appidentity.ProvisioningService
}

func NewIdentityHandler(service *appidentity.ProvisioningService) (*IdentityHandler, error) {
	if service == nil {
		return nil, errors.New("identity provisioning service is required")
	}
	return &IdentityHandler{service: service}, nil
}

func (h *IdentityHandler) writeError(w http.ResponseWriter, err error) {
	writeError(w, err)
}

func (h *IdentityHandler) ListPlatformUsers(w http.ResponseWriter, r *http.Request, params generated.ListPlatformUsersParams) {
	filter := appidentity.UserFilter{Limit: intValue(params.Limit)}
	if params.Search != nil {
		filter.Search = *params.Search
	}
	if params.Status != nil {
		filter.Status = string(*params.Status)
	}
	if params.Cursor != nil {
		cursor, err := pagination.DecodeUUID(*params.Cursor)
		if err != nil {
			h.writeError(w, err)
			return
		}
		filter.Cursor = &cursor
	}
	users, next, err := h.service.ListPlatformUsers(r.Context(), filter)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := platformUserListResponse{
		Data: make([]platformUserSummary, 0, len(users)),
		Pagination: paginationResponse{
			NextCursor: stringPtr(next),
		},
	}
	for _, user := range users {
		response.Data = append(response.Data, platformUserSummaryFromDomain(user))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *IdentityHandler) CreatePlatformUser(w http.ResponseWriter, r *http.Request) {
	var body generated.CreatePlatformUserJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	var displayName string
	if body.DisplayName != nil {
		displayName = *body.DisplayName
	}
	user, err := h.service.CreatePlatformUser(r.Context(), appidentity.CreatePlatformUserInput{Email: string(body.Email), DisplayName: displayName})
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/platform/users/"+user.ID.String())
	writeJSON(w, http.StatusCreated, platformUserDetailFromDomain(user))
}

func (h *IdentityHandler) GetPlatformUser(w http.ResponseWriter, r *http.Request, userID openapi_types.UUID) {
	id, err := domain.ParseUserID(userID.String())
	if err != nil {
		h.writeError(w, err)
		return
	}
	user, err := h.service.GetPlatformUser(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, platformUserDetailFromDomain(user))
}

func (h *IdentityHandler) BlockPlatformUser(w http.ResponseWriter, r *http.Request, userID openapi_types.UUID) {
	id, err := domain.ParseUserID(userID.String())
	if err != nil {
		h.writeError(w, err)
		return
	}
	user, err := h.service.BlockPlatformUser(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, platformUserDetailFromDomain(user))
}

func (h *IdentityHandler) ListPlatformTenantMemberships(w http.ResponseWriter, r *http.Request, tenantID openapi_types.UUID, params generated.ListPlatformTenantMembershipsParams) {
	filter, err := membershipFilter(params.Limit, params.Cursor)
	if err != nil {
		h.writeError(w, err)
		return
	}
	result, err := h.service.ListPlatformTenantMemberships(r.Context(), tenantID, filter)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, membershipListResponse(result))
}

func (h *IdentityHandler) CreatePlatformTenantMembership(w http.ResponseWriter, r *http.Request, tenantID openapi_types.UUID) {
	var body generated.CreatePlatformTenantMembershipJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	membership, err := h.service.CreatePlatformTenantMembership(r.Context(), appidentity.CreateMembershipInput{
		TenantID: tenantID, Email: string(body.Email), DisplayName: optionalStringValue(body.DisplayName), RoleCode: string(body.Role),
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, membershipResponse(membership))
}

func (h *IdentityHandler) UpdatePlatformTenantMembership(w http.ResponseWriter, r *http.Request, tenantID, membershipID openapi_types.UUID) {
	var body generated.UpdatePlatformTenantMembershipJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	membership, err := h.service.UpdatePlatformTenantMembership(r.Context(), tenantID, membershipID, string(body.Role))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, membershipResponse(membership))
}

func (h *IdentityHandler) RevokePlatformTenantMembership(w http.ResponseWriter, r *http.Request, tenantID, membershipID openapi_types.UUID) {
	membership, err := h.service.RevokePlatformTenantMembership(r.Context(), tenantID, membershipID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, membershipResponse(membership))
}

func (h *IdentityHandler) ReactivatePlatformTenantMembership(w http.ResponseWriter, r *http.Request, tenantID, membershipID openapi_types.UUID) {
	membership, err := h.service.ReactivatePlatformTenantMembership(r.Context(), tenantID, membershipID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, membershipResponse(membership))
}

func (h *IdentityHandler) ListTenantMemberships(w http.ResponseWriter, r *http.Request, params generated.ListTenantMembershipsParams) {
	filter, err := membershipFilter(params.Limit, params.Cursor)
	if err != nil {
		h.writeError(w, err)
		return
	}
	result, err := h.service.ListTenantMemberships(r.Context(), filter)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, membershipListResponse(result))
}

func (h *IdentityHandler) CreateTenantMembership(w http.ResponseWriter, r *http.Request, _ generated.CreateTenantMembershipParams) {
	var body generated.CreateTenantMembershipJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	membership, err := h.service.CreateTenantMembership(r.Context(), appidentity.CreateMembershipInput{
		Email: string(body.Email), DisplayName: optionalStringValue(body.DisplayName), RoleCode: string(body.Role),
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, membershipResponse(membership))
}

func (h *IdentityHandler) UpdateTenantMembership(w http.ResponseWriter, r *http.Request, membershipID openapi_types.UUID, _ generated.UpdateTenantMembershipParams) {
	var body generated.UpdateTenantMembershipJSONBody
	if err := decodeJSON(r, &body); err != nil {
		h.writeError(w, err)
		return
	}
	membership, err := h.service.UpdateTenantMembership(r.Context(), membershipID, string(body.Role))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, membershipResponse(membership))
}

func (h *IdentityHandler) RevokeTenantMembership(w http.ResponseWriter, r *http.Request, membershipID openapi_types.UUID, _ generated.RevokeTenantMembershipParams) {
	membership, err := h.service.RevokeTenantMembership(r.Context(), membershipID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, membershipResponse(membership))
}

func (h *IdentityHandler) ReactivateTenantMembership(w http.ResponseWriter, r *http.Request, membershipID openapi_types.UUID, _ generated.ReactivateTenantMembershipParams) {
	membership, err := h.service.ReactivateTenantMembership(r.Context(), membershipID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, membershipResponse(membership))
}

type platformUserSummary struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"displayName"`
	Status       string    `json:"status"`
	GoogleLinked bool      `json:"googleLinked"`
}

type platformUserDetail struct {
	platformUserSummary
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type membershipResponseDTO struct {
	ID           uuid.UUID `json:"id"`
	UserID       uuid.UUID `json:"userId"`
	TenantID     uuid.UUID `json:"tenantId"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"displayName"`
	UserStatus   string    `json:"userStatus"`
	GoogleLinked bool      `json:"googleLinked"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type membershipListResponseDTO struct {
	Data       []membershipResponseDTO `json:"data"`
	Pagination paginationResponse      `json:"pagination"`
}

type platformUserListResponse struct {
	Data       []platformUserSummary `json:"data"`
	Pagination paginationResponse    `json:"pagination"`
}

func platformUserSummaryFromDomain(user domain.User) platformUserSummary {
	return platformUserSummary{
		ID:           user.ID.UUID(),
		Email:        user.Email,
		DisplayName:  user.DisplayName,
		Status:       string(user.Status),
		GoogleLinked: user.GoogleSubject != "",
	}
}

func platformUserDetailFromDomain(user domain.User) platformUserDetail {
	return platformUserDetail{
		platformUserSummary: platformUserSummaryFromDomain(user),
		CreatedAt:           user.CreatedAt,
		UpdatedAt:           user.UpdatedAt,
	}
}

func membershipResponse(membership appidentity.Membership) membershipResponseDTO {
	return membershipResponseDTO{
		ID:           membership.ID,
		UserID:       membership.UserID,
		TenantID:     membership.TenantID,
		Email:        membership.UserEmail,
		DisplayName:  membership.UserDisplayName,
		UserStatus:   membership.UserStatus,
		GoogleLinked: membership.GoogleLinked,
		Role:         membership.RoleCode,
		Status:       membership.Status,
		CreatedAt:    membership.CreatedAt,
		UpdatedAt:    membership.UpdatedAt,
	}
}

func membershipListResponse(result appidentity.MembershipListResult) membershipListResponseDTO {
	data := make([]membershipResponseDTO, 0, len(result.Items))
	for _, membership := range result.Items {
		data = append(data, membershipResponse(membership))
	}
	return membershipListResponseDTO{Data: data, Pagination: paginationResponse{NextCursor: stringPtr(result.NextCursor)}}
}

func membershipFilter(limit *int32, cursor *string) (appidentity.TenantMembershipFilter, error) {
	filter := appidentity.TenantMembershipFilter{Limit: intValue(limit)}
	if cursor != nil {
		id, err := pagination.DecodeUUID(*cursor)
		if err != nil {
			return filter, err
		}
		filter.Cursor = &id
	}
	return filter, nil
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
