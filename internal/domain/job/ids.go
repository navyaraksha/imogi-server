package job

import (
	"fmt"

	"github.com/google/uuid"

	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
)

type ID uuid.UUID
type AttemptID uuid.UUID

func NewID() (ID, error) {
	id, err := identitydomain.NewUUIDv7()
	return ID(id), err
}

func NewAttemptID() (AttemptID, error) {
	id, err := identitydomain.NewUUIDv7()
	return AttemptID(id), err
}

func ParseID(value string) (ID, error) {
	id, err := identitydomain.ParseUUIDv7(value)
	if err != nil {
		return ID(uuid.Nil), fmt.Errorf("job id: %w", err)
	}
	return ID(id), nil
}

func (id ID) UUID() uuid.UUID        { return uuid.UUID(id) }
func (id ID) String() string         { return id.UUID().String() }
func (id AttemptID) UUID() uuid.UUID { return uuid.UUID(id) }
func (id AttemptID) String() string  { return id.UUID().String() }
