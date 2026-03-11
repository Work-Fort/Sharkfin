// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, it, expect, afterEach } from "vitest";
import { WebSocketServer, type WebSocket as WSType } from "ws";
import { SharkfinClient } from "../src/client.js";
import { SharkfinError } from "../src/errors.js";

let wss: WebSocketServer;
let client: SharkfinClient;

function startServer(handler: (ws: WSType) => void): Promise<string> {
  return new Promise((resolve) => {
    wss = new WebSocketServer({ port: 0 }, () => {
      const port = (wss.address() as { port: number }).port;
      resolve(`ws://localhost:${port}`);
    });
    wss.on("connection", handler);
  });
}

function sendHello(ws: WSType) {
  ws.send(
    JSON.stringify({
      type: "hello",
      d: { heartbeat_interval: 10, version: "v0.1.0" },
    }),
  );
}

function readReqAndReply(ws: WSType, response: unknown) {
  return new Promise<void>((resolve) => {
    ws.once("message", (data) => {
      const req = JSON.parse(data.toString());
      ws.send(
        JSON.stringify({
          type: "reply",
          ref: req.ref,
          ok: true,
          d: response,
        }),
      );
      resolve();
    });
  });
}

function readReqAndError(ws: WSType, message: string) {
  return new Promise<void>((resolve) => {
    ws.once("message", (data) => {
      const req = JSON.parse(data.toString());
      ws.send(
        JSON.stringify({
          type: "reply",
          ref: req.ref,
          ok: false,
          d: { message },
        }),
      );
      resolve();
    });
  });
}

afterEach(() => {
  client?.close();
  wss?.close();
});

describe("SharkfinClient", () => {
  it("connects and reads hello", async () => {
    const url = await startServer((ws) => {
      sendHello(ws);
    });

    client = new SharkfinClient(url, { WebSocket: (await import("ws")).WebSocket as unknown as typeof globalThis.WebSocket });
    await client.connect();

    expect(client.serverVersion).toBe("v0.1.0");
    expect(client.heartbeatInterval).toBe(10);
  });

  it("register succeeds", async () => {
    const url = await startServer((ws) => {
      sendHello(ws);
      readReqAndReply(ws, null);
    });

    client = new SharkfinClient(url, { WebSocket: (await import("ws")).WebSocket as unknown as typeof globalThis.WebSocket });
    await client.connect();
    await client.register("alice");
  });

  it("server error throws SharkfinError", async () => {
    const url = await startServer((ws) => {
      sendHello(ws);
      readReqAndError(ws, "user not found");
    });

    client = new SharkfinClient(url, { WebSocket: (await import("ws")).WebSocket as unknown as typeof globalThis.WebSocket });
    await client.connect();

    try {
      await client.identify("nonexistent");
      expect.unreachable("should have thrown");
    } catch (err) {
      expect(err).toBeInstanceOf(SharkfinError);
      expect((err as SharkfinError).serverMessage).toBe("user not found");
    }
  });

  it("receives message.new event", async () => {
    const url = await startServer((ws) => {
      sendHello(ws);
      setTimeout(() => {
        ws.send(
          JSON.stringify({
            type: "message.new",
            d: {
              id: 1,
              channel: "general",
              channel_type: "channel",
              from: "alice",
              body: "hello",
              sent_at: "2026-03-11T00:00:00Z",
            },
          }),
        );
      }, 50);
    });

    client = new SharkfinClient(url, { WebSocket: (await import("ws")).WebSocket as unknown as typeof globalThis.WebSocket });
    await client.connect();

    const msg = await new Promise<any>((resolve) => {
      client.on("message", resolve);
    });

    expect(msg.from).toBe("alice");
    expect(msg.channel).toBe("general");
    expect(msg.channelType).toBe("channel"); // snake_case -> camelCase
  });

  it("receives presence event", async () => {
    const url = await startServer((ws) => {
      sendHello(ws);
      setTimeout(() => {
        ws.send(
          JSON.stringify({
            type: "presence",
            d: { username: "bob", status: "offline" },
          }),
        );
      }, 50);
    });

    client = new SharkfinClient(url, { WebSocket: (await import("ws")).WebSocket as unknown as typeof globalThis.WebSocket });
    await client.connect();

    const p = await new Promise<any>((resolve) => {
      client.on("presence", resolve);
    });

    expect(p.username).toBe("bob");
    expect(p.status).toBe("offline");
  });

  it("users returns user list", async () => {
    const url = await startServer((ws) => {
      sendHello(ws);
      readReqAndReply(ws, {
        users: [
          { username: "alice", online: true, type: "human", state: "active" },
          { username: "bob", online: false, type: "human" },
        ],
      });
    });

    client = new SharkfinClient(url, { WebSocket: (await import("ws")).WebSocket as unknown as typeof globalThis.WebSocket });
    await client.connect();

    const users = await client.users();
    expect(users).toHaveLength(2);
    expect(users[0].username).toBe("alice");
    expect(users[0].online).toBe(true);
  });

  it("sendMessage returns id", async () => {
    const url = await startServer((ws) => {
      sendHello(ws);
      readReqAndReply(ws, { id: 42 });
    });

    client = new SharkfinClient(url, { WebSocket: (await import("ws")).WebSocket as unknown as typeof globalThis.WebSocket });
    await client.connect();

    const id = await client.sendMessage("general", "hello");
    expect(id).toBe(42);
  });

  it("history returns messages with camelCase keys", async () => {
    const url = await startServer((ws) => {
      sendHello(ws);
      readReqAndReply(ws, {
        channel: "general",
        messages: [
          { id: 1, from: "alice", body: "hi", sent_at: "2026-03-11T00:00:00Z" },
        ],
      });
    });

    client = new SharkfinClient(url, { WebSocket: (await import("ws")).WebSocket as unknown as typeof globalThis.WebSocket });
    await client.connect();

    const msgs = await client.history("general");
    expect(msgs).toHaveLength(1);
    expect(msgs[0].from).toBe("alice");
    expect(msgs[0].sentAt).toBe("2026-03-11T00:00:00Z"); // snake -> camel
  });

  it("channels returns channel list", async () => {
    const url = await startServer((ws) => {
      sendHello(ws);
      readReqAndReply(ws, {
        channels: [{ name: "general", public: true, member: true }],
      });
    });

    client = new SharkfinClient(url, { WebSocket: (await import("ws")).WebSocket as unknown as typeof globalThis.WebSocket });
    await client.connect();

    const channels = await client.channels();
    expect(channels).toHaveLength(1);
    expect(channels[0].name).toBe("general");
    expect(channels[0].public).toBe(true);
  });

  it("dmOpen returns result", async () => {
    const url = await startServer((ws) => {
      sendHello(ws);
      readReqAndReply(ws, {
        channel: "dm_alice_bob",
        participant: "bob",
        created: true,
      });
    });

    client = new SharkfinClient(url, { WebSocket: (await import("ws")).WebSocket as unknown as typeof globalThis.WebSocket });
    await client.connect();

    const result = await client.dmOpen("bob");
    expect(result.channel).toBe("dm_alice_bob");
    expect(result.created).toBe(true);
  });

  it("unreadCounts returns counts with camelCase keys", async () => {
    const url = await startServer((ws) => {
      sendHello(ws);
      readReqAndReply(ws, {
        counts: [
          { channel: "general", type: "channel", unread_count: 5, mention_count: 1 },
        ],
      });
    });

    client = new SharkfinClient(url, { WebSocket: (await import("ws")).WebSocket as unknown as typeof globalThis.WebSocket });
    await client.connect();

    const counts = await client.unreadCounts();
    expect(counts).toHaveLength(1);
    expect(counts[0].unreadCount).toBe(5); // snake -> camel
    expect(counts[0].mentionCount).toBe(1);
  });

  it("interleaved broadcasts don't break requests", async () => {
    const url = await startServer((ws) => {
      sendHello(ws);
      ws.once("message", (data) => {
        const req = JSON.parse(data.toString());
        // Send broadcast before reply.
        ws.send(JSON.stringify({
          type: "presence",
          d: { username: "bob", status: "online", state: "active" },
        }));
        // Then reply.
        ws.send(JSON.stringify({
          type: "reply", ref: req.ref, ok: true, d: null,
        }));
      });
    });

    client = new SharkfinClient(url, { WebSocket: (await import("ws")).WebSocket as unknown as typeof globalThis.WebSocket });
    await client.connect();

    await client.register("alice");
    // Should not throw despite interleaved broadcast.
  });
});
