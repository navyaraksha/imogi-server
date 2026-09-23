package job

import "errors"

type RetryableError struct{ Err error }

func (err RetryableError) Error() string { return err.Err.Error() }
func (err RetryableError) Unwrap() error { return err.Err }

func Retry(err error) error {
	if err == nil {
		return nil
	}
	return RetryableError{Err: err}
}

func retryable(err error) bool {
	var retryError RetryableError
	return errors.As(err, &retryError)
}
