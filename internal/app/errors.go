package app

import "errors"

type ErrorClass string

const (
	ErrorRecoverable ErrorClass = "recoverable"
	ErrorFatal       ErrorClass = "fatal"
)

func ClassifyError(err error) ErrorClass {
	if err == nil {
		return ErrorRecoverable
	}
	if errors.Is(err, ErrServerClosed) {
		return ErrorRecoverable
	}
	return ErrorFatal
}
