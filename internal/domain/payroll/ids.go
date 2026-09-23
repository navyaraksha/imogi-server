package payroll

import (
	"fmt"

	"github.com/google/uuid"

	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
)

type PayrollPeriodID uuid.UUID
type PayrollResultID uuid.UUID
type PayrollResultItemID uuid.UUID

func newID() (uuid.UUID, error) {
	return identitydomain.NewUUIDv7()
}

func NewPayrollPeriodID() (PayrollPeriodID, error) {
	id, err := newID()
	return PayrollPeriodID(id), err
}

func NewPayrollResultID() (PayrollResultID, error) {
	id, err := newID()
	return PayrollResultID(id), err
}

func NewPayrollResultItemID() (PayrollResultItemID, error) {
	id, err := newID()
	return PayrollResultItemID(id), err
}

func ParsePayrollPeriodID(value string) (PayrollPeriodID, error) {
	id, err := identitydomain.ParseUUIDv7(value)
	if err != nil {
		return PayrollPeriodID(uuid.Nil), fmt.Errorf("payroll period id: %w", err)
	}
	return PayrollPeriodID(id), nil
}

func ParsePayrollResultID(value string) (PayrollResultID, error) {
	id, err := identitydomain.ParseUUIDv7(value)
	if err != nil {
		return PayrollResultID(uuid.Nil), fmt.Errorf("payroll result id: %w", err)
	}
	return PayrollResultID(id), nil
}

func (id PayrollPeriodID) UUID() uuid.UUID     { return uuid.UUID(id) }
func (id PayrollResultID) UUID() uuid.UUID     { return uuid.UUID(id) }
func (id PayrollResultItemID) UUID() uuid.UUID { return uuid.UUID(id) }
func (id PayrollPeriodID) String() string      { return id.UUID().String() }
func (id PayrollResultID) String() string      { return id.UUID().String() }
func (id PayrollResultItemID) String() string  { return id.UUID().String() }
