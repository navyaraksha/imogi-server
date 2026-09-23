package employee

import (
	"fmt"
	"regexp"
	"strings"
)

type NIK string
type NPWP string

var nikPattern = regexp.MustCompile(`^[0-9]{16}$`)

func ParseNIK(value string) (NIK, error) {
	if !nikPattern.MatchString(value) {
		return "", fmt.Errorf("%w: NIK must contain exactly 16 digits", ErrInvalidEmployee)
	}
	return NIK(value), nil
}

func (n NIK) String() string { return string(n) }

func ParseNPWP(value string) (NPWP, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: NPWP cannot be blank", ErrInvalidTaxProfile)
	}
	if len(value) > 32 {
		return "", fmt.Errorf("%w: NPWP is too long", ErrInvalidTaxProfile)
	}
	return NPWP(value), nil
}

func (n NPWP) String() string { return string(n) }

type Gender string

const (
	GenderMale        Gender = "male"
	GenderFemale      Gender = "female"
	GenderUnspecified Gender = "unspecified"
)

func ParseGender(value string) (Gender, error) {
	g := Gender(value)
	switch g {
	case GenderMale, GenderFemale, GenderUnspecified:
		return g, nil
	default:
		return "", fmt.Errorf("%w: unsupported gender", ErrInvalidEmployee)
	}
}

type EmploymentType string

const (
	EmploymentPermanent    EmploymentType = "permanent"
	EmploymentNonPermanent EmploymentType = "non_permanent"
)

func ParseEmploymentType(value string) (EmploymentType, error) {
	t := EmploymentType(value)
	switch t {
	case EmploymentPermanent, EmploymentNonPermanent:
		return t, nil
	default:
		return "", fmt.Errorf("%w: unsupported employment type", ErrInvalidEmployment)
	}
}

func validateText(value, field string, max int, err error) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: %s cannot be blank", err, field)
	}
	if len(value) > max {
		return "", fmt.Errorf("%w: %s is too long", err, field)
	}
	return value, nil
}
