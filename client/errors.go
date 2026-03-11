// SPDX-License-Identifier: AGPL-3.0-or-later
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
)

// ServerError is returned when the server replies with ok:false.
type ServerError struct {
	Message string
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("sharkfin: %s", e.Message)
}
