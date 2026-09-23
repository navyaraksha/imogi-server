package filebatch

import "errors"

var (
	ErrBatchNotFound           = errors.New("import batch not found")
	ErrFileObjectNotFound      = errors.New("file object not found")
	ErrBatchInvalidState       = errors.New("import batch is in an invalid state")
	ErrBatchValidationRequired = errors.New("import batch requires successful validation")
	ErrBatchAlreadyCommitted   = errors.New("import batch is already committed")
	ErrUnsupportedOperation    = errors.New("unsupported import operation")
	ErrUnsupportedFileFormat   = errors.New("unsupported file format")
	ErrInvalidFileObject       = errors.New("invalid file object")
	ErrInvalidBatch            = errors.New("invalid import batch")
	ErrValidationFailed        = errors.New("file validation failed")
)
