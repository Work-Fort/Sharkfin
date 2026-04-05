// SPDX-License-Identifier: AGPL-3.0-or-later

// Package client provides a typed Go client for the Sharkfin messaging
// server. It connects via WebSocket for real-time events and provides
// REST methods for service-to-service operations (webhooks, identity
// registration, channel management).
//
// Usage:
//
//	c, err := client.Dial(ctx, "ws://localhost:16000/ws")
//	if err != nil { log.Fatal(err) }
//	defer c.Close()
//
//	if err := c.Register(ctx, "alice", nil); err != nil { log.Fatal(err) }
//
//	go func() {
//	    for ev := range c.Events() {
//	        switch ev.Type {
//	        case "message.new":
//	            msg, _ := ev.AsMessage()
//	            fmt.Printf("%s: %s\n", msg.From, msg.Body)
//	        }
//	    }
//	}()
package client
