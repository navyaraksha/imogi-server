package identity

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var ErrInvalidUUIDv7 = errors.New("invalid UUIDv7")

func NewUUIDv7() (uuid.UUID, error) {
	return uuid.NewV7()
}

func ParseUUIDv7(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", ErrInvalidUUIDv7, err)
	}
	if id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		return uuid.Nil, ErrInvalidUUIDv7
	}
	return id, nil
}
