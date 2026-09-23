package payroll

import "errors"

var (
	ErrPayrollPeriodNotFound         = errors.New("payroll period not found")
	ErrPayrollPeriodAlreadyExists    = errors.New("payroll period already exists")
	ErrPayrollPeriodAlreadyFinalized = errors.New("payroll period is already finalized")
	ErrPayrollPeriodNotOpen          = errors.New("payroll period is not open")
	ErrPayrollResultAlreadyExists    = errors.New("payroll result already exists")
	ErrInvalidPayrollPeriod          = errors.New("invalid payroll period")
	ErrInvalidPayrollResult          = errors.New("invalid payroll result")
	ErrInvalidPayrollComponent       = errors.New("invalid payroll component")
	ErrEmploymentOutsidePeriod       = errors.New("employment does not cover payroll period")
	ErrInvalidPayrollCursor          = errors.New("invalid payroll cursor")
)
