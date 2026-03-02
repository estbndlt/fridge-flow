package service

import "errors"

var ErrUnauthorized = errors.New("unauthorized")

type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string {
	return e.Message
}

func IsValidationError(err error) bool {
	var validation ValidationError
	return errors.As(err, &validation)
}
