// SPDX-License-Identifier: Apache-2.0

// Package client provides a typed Go client for the Sharkfin messaging
// server.
//
// There are two client types:
//
//   - *Client — connects via WebSocket for real-time events and also
//     offers a handful of REST methods (webhook registration,
//     identity registration). Use this when you need the server's
//     event stream.
//
//   - *RESTClient — HTTP-only, no WebSocket. Use this when your
//     service receives events via a registered webhook and therefore
//     does not need the WS event channel. No goroutines, no Dial
//     step, no reconnection state.
//
// Usage (WebSocket):
//
//	c, err := client.Dial(ctx, "ws://localhost:16000/ws", client.WithToken(tok))
//	if err != nil { log.Fatal(err) }
//	defer c.Close()
//
//	for ev := range c.Events() {
//	    switch ev.Type {
//	    case "message.new":
//	        msg, _ := ev.AsMessage()
//	        fmt.Printf("%s: %s\n", msg.From, msg.Body)
//	    }
//	}
//
// Usage (REST-only):
//
//	c := client.NewRESTClient("http://localhost:16000", client.WithToken(tok))
//	defer c.Close()
//
//	if err := c.Register(ctx); err != nil { log.Fatal(err) }
//	if err := c.CreateChannel(ctx, "general", true); err != nil { log.Fatal(err) }
//	id, err := c.SendMessage(ctx, "general", "hello", nil)
//	if err != nil { log.Fatal(err) }
//	_ = id
package client
