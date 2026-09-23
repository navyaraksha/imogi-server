package employee

import "errors"

var (
	ErrEmployeeNotFound            = errors.New("employee not found")
	ErrEmploymentNotFound          = errors.New("employment not found")
	ErrAssignmentNotFound          = errors.New("assignment not found")
	ErrTaxProfileNotFound          = errors.New("tax profile not found")
	ErrEmployeeNumberTaken         = errors.New("employee number already exists")
	ErrNIKAlreadyRegistered        = errors.New("NIK already registered")
	ErrActiveEmploymentExists      = errors.New("active employment already exists")
	ErrEmploymentAlreadyEnded      = errors.New("employment has already ended")
	ErrNoPreviousEmployment        = errors.New("employee has no ended previous employment")
	ErrEmploymentOverlap           = errors.New("employment period overlaps another employment")
	ErrAssignmentOverlap           = errors.New("assignment period overlaps another assignment")
	ErrTaxProfileOverlap           = errors.New("tax profile period overlaps another tax profile")
	ErrInvalidEmployee             = errors.New("invalid employee")
	ErrInvalidEmployment           = errors.New("invalid employment")
	ErrInvalidAssignment           = errors.New("invalid assignment")
	ErrInvalidTaxProfile           = errors.New("invalid tax profile")
	ErrAssignmentOutsideEmployment = errors.New("assignment is outside employment period")
	ErrInvalidCursor               = errors.New("invalid employee cursor")
)
