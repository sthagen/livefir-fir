package errors

import (
	"errors"
	"strings"
)

type Fields map[string]error

func (f Fields) Map() map[string]string {
	m := map[string]string{}
	for field, err := range f {
		m[field] = err.Error()
	}
	return m
}

func (f Fields) Error() string {
	var errs []string
	for _, err := range f {
		errs = append(errs, err.Error())
	}
	return strings.Join(errs, ", ")
}

func User(err error) error {
	userError := err
	if wrappedUserError := errors.Unwrap(err); wrappedUserError != nil {
		userError = wrappedUserError
	}
	return userError
}
