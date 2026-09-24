package filebatch

import "errors"

var (
	ErrBatchNotFound           = errors.New("import batch not found")
	ErrFileObjectNotFound      = errors.New("file object not found")
	ErrTemplateNotFound        = errors.New("import template not found")
	ErrTemplateNotApplicable   = errors.New("import template is not applicable")
	ErrInvalidTemplate         = errors.New("invalid import template")
	ErrBatchInvalidState       = errors.New("import batch is in an invalid state")
	ErrBatchValidationRequired = errors.New("import batch requires successful validation")
	ErrBatchAlreadyCommitted   = errors.New("import batch is already committed")
	ErrUnsupportedOperation    = errors.New("unsupported import operation")
	ErrUnsupportedFileFormat   = errors.New("unsupported file format")
	ErrInvalidFileObject       = errors.New("invalid file object")
	ErrInvalidBatch            = errors.New("invalid import batch")
	ErrValidationFailed        = errors.New("file validation failed")
)
