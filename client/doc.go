// SPDX-License-Identifier: AGPL-3.0-or-later

// Package client provides a typed Go WebSocket client for the Sharkfin
// messaging server. It connects to the /ws endpoint, handles the hello
// handshake and heartbeat automatically, and delivers server-pushed
// events via a channel.
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
