package job

import "errors"

var (
	ErrJobNotFound       = errors.New("background job not found")
	ErrJobNotLeased      = errors.New("background job is not leased by this worker")
	ErrJobAlreadyDone    = errors.New("background job is already completed")
	ErrInvalidJob        = errors.New("invalid background job")
	ErrInvalidJobPayload = errors.New("invalid background job payload")
)
