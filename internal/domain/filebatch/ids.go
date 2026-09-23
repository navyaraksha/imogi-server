package filebatch

import (
	"fmt"

	"github.com/google/uuid"

	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
)

type FileObjectID uuid.UUID
type BatchID uuid.UUID
type ArtifactID uuid.UUID
type TemplateID uuid.UUID
type StepID uuid.UUID

func newUUIDv7() (uuid.UUID, error) { return identitydomain.NewUUIDv7() }

func NewFileObjectID() (FileObjectID, error) { id, err := newUUIDv7(); return FileObjectID(id), err }
func NewBatchID() (BatchID, error)           { id, err := newUUIDv7(); return BatchID(id), err }
func NewArtifactID() (ArtifactID, error)     { id, err := newUUIDv7(); return ArtifactID(id), err }
func NewTemplateID() (TemplateID, error)     { id, err := newUUIDv7(); return TemplateID(id), err }
func NewStepID() (StepID, error)             { id, err := newUUIDv7(); return StepID(id), err }

func ParseBatchID(value string) (BatchID, error) {
	id, err := identitydomain.ParseUUIDv7(value)
	if err != nil {
		return BatchID(uuid.Nil), fmt.Errorf("batch id: %w", err)
	}
	return BatchID(id), nil
}

func (id FileObjectID) UUID() uuid.UUID { return uuid.UUID(id) }
func (id BatchID) UUID() uuid.UUID      { return uuid.UUID(id) }
func (id ArtifactID) UUID() uuid.UUID   { return uuid.UUID(id) }
func (id TemplateID) UUID() uuid.UUID   { return uuid.UUID(id) }
func (id StepID) UUID() uuid.UUID       { return uuid.UUID(id) }

func (id FileObjectID) String() string { return id.UUID().String() }
func (id BatchID) String() string      { return id.UUID().String() }
func (id ArtifactID) String() string   { return id.UUID().String() }
func (id TemplateID) String() string   { return id.UUID().String() }
func (id StepID) String() string       { return id.UUID().String() }
