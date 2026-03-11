// SPDX-License-Identifier: AGPL-3.0-or-later
package client

import "encoding/json"

// Event is a server-pushed message delivered via the Events() channel.
type Event struct {
	// Type is the envelope type, e.g. "message.new", "presence".
	Type string

	// Data is the raw JSON payload from the "d" field.
	Data json.RawMessage
}

// AsMessage decodes the event data as a BroadcastMessage.
// Returns an error if the data does not match.
func (e Event) AsMessage() (*BroadcastMessage, error) {
	var m BroadcastMessage
	if err := json.Unmarshal(e.Data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// AsPresence decodes the event data as a PresenceUpdate.
func (e Event) AsPresence() (*PresenceUpdate, error) {
	var p PresenceUpdate
	if err := json.Unmarshal(e.Data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
