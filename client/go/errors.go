// SPDX-License-Identifier: Apache-2.0
package client

import (
	"errors"
	"fmt"
)

var (
	// ErrNotConnected is returned when a method is called on a closed or
	// disconnected client.
	ErrNotConnected = errors.New("client: not connected")

	// ErrTimeout is returned when a request does not receive a reply
	// within the context deadline.
	ErrTimeout = errors.New("client: request timeout")

	// ErrClosed is returned when operations are attempted on a closed client.
	ErrClosed = errors.New("client: closed")

	// ErrBadRequest wraps HTTP 400 responses.
	ErrBadRequest = errors.New("client: bad request")

	// ErrUnauthorized wraps HTTP 401 responses.
	ErrUnauthorized = errors.New("client: unauthorized")

	// ErrNotFound wraps HTTP 404 responses.
	ErrNotFound = errors.New("client: not found")

	// ErrConflict wraps HTTP 409 responses.
	ErrConflict = errors.New("client: conflict")
)

// ServerError is returned when the server replies with an error.
// For WS replies this is constructed from an ok:false envelope and
// leaves Status zero. For REST replies Status carries the HTTP code
// and (when the code maps to one) wrapped is set to a sentinel so
// errors.Is(err, ErrNotFound) and friends work.
type ServerError struct {
	Message string
	Status  int

	wrapped error
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("sharkfin: %s", e.Message)
}

// Unwrap returns the wrapped sentinel error if any. Enables
// errors.Is(err, ErrNotFound) etc. for REST callers.
func (e *ServerError) Unwrap() error {
	return e.wrapped
}
