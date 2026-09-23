package restapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	appidentity "github.com/navyaraksha/imogi/internal/application/identity"
	appjob "github.com/navyaraksha/imogi/internal/application/job"
	domain "github.com/navyaraksha/imogi/internal/domain/employee"
	filebatchdomain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	jobdomain "github.com/navyaraksha/imogi/internal/domain/job"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	payrolldomain "github.com/navyaraksha/imogi/internal/domain/payroll"
	"github.com/navyaraksha/imogi/internal/platform/security"
	generated "github.com/navyaraksha/imogi/internal/restapi/generated"
)

func writeError(w http.ResponseWriter, err error) {
	status, code, message := mapError(err)
	writeJSON(w, status, generated.ErrorResponse{
		Code:    code,
		Message: message,
		Details: map[string]interface{}{},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func mapError(err error) (int, string, string) {
	switch {
	case errors.Is(err, security.ErrUnauthenticated):
		return http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required"
	case errors.Is(err, security.ErrTenantSelectionRequired), errors.Is(err, appidentity.ErrActiveTenantRequired):
		return http.StatusBadRequest, "ACTIVE_TENANT_REQUIRED", "An active tenant must be selected"
	case errors.Is(err, appidentity.ErrIdentityNotProvisioned):
		return http.StatusForbidden, "IDENTITY_NOT_PROVISIONED", "The authenticated identity has no provisioned access"
	case errors.Is(err, appidentity.ErrIdentityBlocked):
		return http.StatusForbidden, "IDENTITY_BLOCKED", "The authenticated identity is blocked"
	case errors.Is(err, appidentity.ErrTenantAccessDenied):
		return http.StatusForbidden, "TENANT_ACCESS_DENIED", "The authenticated identity has no access to the selected tenant"
	case errors.Is(err, appidentity.ErrIdentityConflict):
		return http.StatusForbidden, "IDENTITY_CONFLICT", "The Google identity conflicts with the provisioned account"
	case errors.Is(err, security.ErrForbidden):
		return http.StatusForbidden, "FORBIDDEN", "You do not have permission to perform this action"
	case isNotFoundError(err):
		return http.StatusNotFound, "NOT_FOUND", "The requested resource was not found"
	case isConflictError(err):
		return http.StatusConflict, "CONFLICT", "The requested change conflicts with existing data"
	case isInvalidRequestError(err):
		return http.StatusBadRequest, "INVALID_REQUEST", "The request is invalid"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "An internal error occurred"
	}
}

func isNotFoundError(err error) bool {
	return errors.Is(err, appidentity.ErrPlatformUserNotFound) ||
		errors.Is(err, appidentity.ErrTenantMembershipNotFound) ||
		errors.Is(err, appidentity.ErrTenantNotFound) ||
		errors.Is(err, domain.ErrEmployeeNotFound) ||
		errors.Is(err, domain.ErrEmploymentNotFound) ||
		errors.Is(err, domain.ErrAssignmentNotFound) ||
		errors.Is(err, domain.ErrTaxProfileNotFound) ||
		errors.Is(err, organization.ErrTenantNotFound) ||
		errors.Is(err, organization.ErrCompanyNotFound) ||
		errors.Is(err, organization.ErrUnitNotFound) ||
		errors.Is(err, payrolldomain.ErrPayrollPeriodNotFound) ||
		errors.Is(err, filebatchdomain.ErrBatchNotFound) ||
		errors.Is(err, filebatchdomain.ErrFileObjectNotFound) ||
		errors.Is(err, jobdomain.ErrJobNotFound)
}

func isConflictError(err error) bool {
	return errors.Is(err, appidentity.ErrPlatformUserAlreadyExists) ||
		errors.Is(err, appidentity.ErrMembershipAlreadyExists) ||
		errors.Is(err, appidentity.ErrCannotBlockCurrentUser) ||
		errors.Is(err, domain.ErrEmployeeNumberTaken) ||
		errors.Is(err, domain.ErrNIKAlreadyRegistered) ||
		errors.Is(err, domain.ErrActiveEmploymentExists) ||
		errors.Is(err, domain.ErrEmploymentAlreadyEnded) ||
		errors.Is(err, domain.ErrEmploymentOverlap) ||
		errors.Is(err, domain.ErrAssignmentOverlap) ||
		errors.Is(err, domain.ErrTaxProfileOverlap) ||
		errors.Is(err, payrolldomain.ErrPayrollPeriodAlreadyExists) ||
		errors.Is(err, payrolldomain.ErrPayrollResultAlreadyExists) ||
		errors.Is(err, payrolldomain.ErrPayrollPeriodAlreadyFinalized) ||
		errors.Is(err, payrolldomain.ErrEmploymentOutsidePeriod) ||
		errors.Is(err, filebatchdomain.ErrBatchInvalidState) ||
		errors.Is(err, filebatchdomain.ErrBatchValidationRequired) ||
		errors.Is(err, filebatchdomain.ErrUnsupportedFileFormat) ||
		errors.Is(err, filebatchdomain.ErrInvalidFileObject) ||
		errors.Is(err, filebatchdomain.ErrUnsupportedOperation) ||
		errors.Is(err, jobdomain.ErrInvalidJob) ||
		errors.Is(err, jobdomain.ErrInvalidJobPayload) ||
		errors.Is(err, appjob.ErrNoJobAvailable)
}

func isInvalidRequestError(err error) bool {
	return errors.Is(err, appidentity.ErrInvalidMembershipRole) ||
		errors.Is(err, domain.ErrNoPreviousEmployment) ||
		errors.Is(err, domain.ErrInvalidEmployee) ||
		errors.Is(err, domain.ErrInvalidEmployment) ||
		errors.Is(err, domain.ErrInvalidAssignment) ||
		errors.Is(err, domain.ErrInvalidTaxProfile) ||
		errors.Is(err, domain.ErrAssignmentOutsideEmployment) ||
		errors.Is(err, domain.ErrInvalidCursor) ||
		errors.Is(err, organization.ErrUnitTypeMismatch) ||
		errors.Is(err, payrolldomain.ErrInvalidPayrollPeriod) ||
		errors.Is(err, payrolldomain.ErrInvalidPayrollResult) ||
		errors.Is(err, payrolldomain.ErrInvalidPayrollComponent) ||
		errors.Is(err, payrolldomain.ErrPayrollPeriodNotOpen) ||
		errors.Is(err, payrolldomain.ErrInvalidPayrollCursor) ||
		strings.Contains(err.Error(), "invalid user") ||
		strings.Contains(err.Error(), "decode cursor") ||
		strings.Contains(err.Error(), "request body") ||
		strings.Contains(err.Error(), "cannot be null") ||
		isJSONDecodeError(err)
}

func isJSONDecodeError(err error) bool {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	return errors.As(err, &syntaxErr) || errors.As(err, &typeErr)
}
