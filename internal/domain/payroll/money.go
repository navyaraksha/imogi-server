package payroll

import "fmt"

// Money is an amount in whole Indonesian Rupiah. V0 does not represent
// fractional Rupiah or use floating-point arithmetic.
type Money int64

func NewMoney(amount int64) (Money, error) {
	if amount < 0 {
		return 0, fmt.Errorf("%w: amount cannot be negative", ErrInvalidPayrollResult)
	}
	return Money(amount), nil
}

func (amount Money) Int64() int64 {
	return int64(amount)
}
