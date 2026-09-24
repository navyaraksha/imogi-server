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
type ImportRowID uuid.UUID
type ImportRowIssueID uuid.UUID

func newUUIDv7() (uuid.UUID, error) {
	return identitydomain.NewUUIDv7()
}

func NewFileObjectID() (FileObjectID, error) {
	id, err := newUUIDv7()
	return FileObjectID(id), err
}

func NewBatchID() (BatchID, error) {
	id, err := newUUIDv7()
	return BatchID(id), err
}

func NewArtifactID() (ArtifactID, error) {
	id, err := newUUIDv7()
	return ArtifactID(id), err
}

func NewTemplateID() (TemplateID, error) {
	id, err := newUUIDv7()
	return TemplateID(id), err
}

func NewStepID() (StepID, error) {
	id, err := newUUIDv7()
	return StepID(id), err
}

func NewImportRowID() (ImportRowID, error) {
	id, err := newUUIDv7()
	return ImportRowID(id), err
}

func NewImportRowIssueID() (ImportRowIssueID, error) {
	id, err := newUUIDv7()
	return ImportRowIssueID(id), err
}

func ParseBatchID(value string) (BatchID, error) {
	id, err := identitydomain.ParseUUIDv7(value)
	if err != nil {
		return BatchID(uuid.Nil), fmt.Errorf("batch id: %w", err)
	}
	return BatchID(id), nil
}

func ParseTemplateID(value string) (TemplateID, error) {
	id, err := identitydomain.ParseUUIDv7(value)
	if err != nil {
		return TemplateID(uuid.Nil), fmt.Errorf("template id: %w", err)
	}
	return TemplateID(id), nil
}

func ParseImportRowID(value string) (ImportRowID, error) {
	id, err := identitydomain.ParseUUIDv7(value)
	if err != nil {
		return ImportRowID(uuid.Nil), fmt.Errorf("import row id: %w", err)
	}
	return ImportRowID(id), nil
}

func (id FileObjectID) UUID() uuid.UUID     { return uuid.UUID(id) }
func (id BatchID) UUID() uuid.UUID          { return uuid.UUID(id) }
func (id ArtifactID) UUID() uuid.UUID       { return uuid.UUID(id) }
func (id TemplateID) UUID() uuid.UUID       { return uuid.UUID(id) }
func (id StepID) UUID() uuid.UUID           { return uuid.UUID(id) }
func (id ImportRowID) UUID() uuid.UUID      { return uuid.UUID(id) }
func (id ImportRowIssueID) UUID() uuid.UUID { return uuid.UUID(id) }

func (id FileObjectID) String() string { return id.UUID().String() }
func (id BatchID) String() string      { return id.UUID().String() }
func (id ArtifactID) String() string   { return id.UUID().String() }
func (id TemplateID) String() string   { return id.UUID().String() }
func (id StepID) String() string       { return id.UUID().String() }
