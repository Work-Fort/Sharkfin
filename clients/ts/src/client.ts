// SPDX-License-Identifier: AGPL-3.0-or-later
import { Emitter } from "./emitter.js";
import { SharkfinError } from "./errors.js";
import type {
  User,
  Channel,
  Message,
  BroadcastMessage,
  PresenceUpdate,
  UnreadCount,
  DM,
  DMOpenResult,
  MentionGroup,
  SendOptions,
  HistoryOptions,
  UnreadOptions,
  ClientOptions,
  Envelope,
} from "./types.js";

type PendingRequest = {
  resolve: (data: unknown) => void;
  reject: (err: Error) => void;
};

/**
 * Convert an object's keys from snake_case to camelCase (shallow).
 */
function toCamel<T>(obj: unknown): T {
  if (typeof obj !== "object" || obj === null || Array.isArray(obj)) return obj as T;
  const result: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
    result[k.replace(/_([a-z])/g, (_, c) => c.toUpperCase())] = v;
  }
  return result as T;
}

/**
 * Convert an array of objects from snake_case to camelCase (shallow per element).
 */
function toCamelArray<T>(arr: unknown[]): T[] {
  return arr.map((item) => toCamel<T>(item));
}

export class SharkfinClient extends Emitter {
  private ws: WebSocket | null = null;
  private pending = new Map<string, PendingRequest>();
  private refSeq = 0;
  private url: string;
  private opts: ClientOptions;
  private _closed = false;

  constructor(url: string, opts: ClientOptions = {}) {
    super();
    this.url = url;
    this.opts = opts;
  }

  /** Connect to the server. Authentication is provided via token or apiKey options. */
  async connect(): Promise<void> {
    this._closed = false;
    const headers: Record<string, string> = {};
    if (this.opts.token) {
      headers["Authorization"] = `Bearer ${this.opts.token}`;
    } else if (this.opts.apiKey) {
      headers["Authorization"] = `Bearer ${this.opts.apiKey}`;
    }

    const WS = this.opts.WebSocket ?? WebSocket;
    const ws = new WS(this.url, { headers });
    this.ws = ws;

    await new Promise<void>((resolve, reject) => {
      const onOpen = () => {
        ws.removeEventListener("open", onOpen as EventListener);
        ws.removeEventListener("error", onError);
        this.setupMessageHandler(ws);
        resolve();
      };

      const onError = () => {
        ws.removeEventListener("open", onOpen as EventListener);
        reject(new Error("Failed to connect"));
      };

      ws.addEventListener("open", onOpen as EventListener);
      ws.addEventListener("error", onError);
    });
  }

  /** Close the connection. */
  close(): void {
    this._closed = true;
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    // Reject all pending requests.
    for (const [, p] of this.pending) {
      p.reject(new Error("client: closed"));
    }
    this.pending.clear();
  }

  private setupMessageHandler(ws: WebSocket): void {
    ws.addEventListener("message", ((event: MessageEvent | { data: string }) => {
      const data = typeof event === "object" && "data" in event ? event.data : event;
      const env: Envelope = JSON.parse(typeof data === "string" ? data : data.toString());

      if (env.ref) {
        // Reply to a pending request.
        const pending = this.pending.get(env.ref);
        if (pending) {
          this.pending.delete(env.ref);
          if (env.ok === false) {
            const d = env.d as { message: string } | null;
            pending.reject(new SharkfinError(d?.message ?? "unknown error"));
          } else {
            pending.resolve(env.d);
          }
        }
      } else {
        // Server push.
        switch (env.type) {
          case "message.new":
            this.emit("message", toCamel<BroadcastMessage>(env.d));
            break;
          case "presence":
            this.emit("presence", toCamel<PresenceUpdate>(env.d));
            break;
          default:
            this.emit(env.type, env.d);
        }
      }
    }) as EventListener);

    ws.addEventListener("close", () => {
      this.emit("disconnect");
      // Reject all pending requests.
      for (const [, p] of this.pending) {
        p.reject(new Error("client: disconnected"));
      }
      this.pending.clear();

      // Handle reconnection if configured.
      if (this.opts.reconnect && !this._closed) {
        this.reconnectLoop();
      }
    });
  }

  private async reconnectLoop(): Promise<void> {
    const backoff =
      typeof this.opts.reconnect === "function"
        ? this.opts.reconnect
        : (attempt: number) => Math.min(1000 * 2 ** attempt, 30000);

    for (let attempt = 0; ; attempt++) {
      if (this._closed) return;
      const delay = backoff(attempt);
      if (delay < 0) return;
      await new Promise((r) => setTimeout(r, delay));
      if (this._closed) return;
      try {
        await this.connect();
        this.emit("reconnect");
        return;
      } catch {
        // retry
      }
    }
  }

  private nextRef(): string {
    return `ref_${++this.refSeq}`;
  }

  private async request(type: string, d?: unknown): Promise<unknown> {
    if (!this.ws) throw new Error("client: not connected");

    const ref = this.nextRef();
    const envelope: Envelope = { type, ref };
    if (d !== undefined && d !== null) {
      envelope.d = d;
    }

    return new Promise<unknown>((resolve, reject) => {
      this.pending.set(ref, { resolve, reject });
      this.ws!.send(JSON.stringify(envelope));
    });
  }

  // --- Users ---

  async users(): Promise<User[]> {
    const data = (await this.request("user_list")) as { users: unknown[] };
    return toCamelArray<User>(data.users);
  }

  // --- Channels ---

  async channels(): Promise<Channel[]> {
    const data = (await this.request("channel_list")) as { channels: unknown[] };
    return toCamelArray<Channel>(data.channels);
  }

  async createChannel(name: string, isPublic: boolean): Promise<void> {
    await this.request("channel_create", { name, public: isPublic });
  }

  async inviteToChannel(channel: string, username: string): Promise<void> {
    await this.request("channel_invite", { channel, username });
  }

  async joinChannel(channel: string): Promise<void> {
    await this.request("channel_join", { channel });
  }

  // --- Messages ---

  async sendMessage(channel: string, body: string, opts?: SendOptions): Promise<number> {
    const d: Record<string, unknown> = { channel, body };
    if (opts?.threadId != null) d.thread_id = opts.threadId;
    const data = (await this.request("send_message", d)) as { id: number };
    return data.id;
  }

  async history(channel: string, opts?: HistoryOptions): Promise<Message[]> {
    const d: Record<string, unknown> = { channel };
    if (opts?.before != null) d.before = opts.before;
    if (opts?.after != null) d.after = opts.after;
    if (opts?.limit != null) d.limit = opts.limit;
    if (opts?.threadId != null) d.thread_id = opts.threadId;
    const data = (await this.request("history", d)) as { messages: unknown[] };
    return toCamelArray<Message>(data.messages);
  }

  async unreadMessages(channel?: string, opts?: UnreadOptions): Promise<Message[]> {
    const d: Record<string, unknown> = {};
    if (channel) d.channel = channel;
    if (opts?.mentionsOnly) d.mentions_only = true;
    if (opts?.threadId != null) d.thread_id = opts.threadId;
    const data = (await this.request("unread_messages", d)) as { messages: unknown[] };
    return toCamelArray<Message>(data.messages);
  }

  async unreadCounts(): Promise<UnreadCount[]> {
    const data = (await this.request("unread_counts")) as { counts: unknown[] };
    return toCamelArray<UnreadCount>(data.counts);
  }

  async markRead(channel: string, messageId?: number): Promise<void> {
    const d: Record<string, unknown> = { channel };
    if (messageId != null && messageId > 0) d.message_id = messageId;
    await this.request("mark_read", d);
  }

  // --- DMs ---

  async dmOpen(username: string): Promise<DMOpenResult> {
    const data = (await this.request("dm_open", { username })) as unknown;
    return toCamel<DMOpenResult>(data);
  }

  async dmList(): Promise<DM[]> {
    const data = (await this.request("dm_list")) as { dms: unknown[] };
    return toCamelArray<DM>(data.dms);
  }

  // --- Presence ---

  async setState(state: string): Promise<void> {
    await this.request("set_state", { state });
  }

  // --- Info ---

  async ping(): Promise<void> {
    await this.request("ping");
  }

  async version(): Promise<string> {
    const data = (await this.request("version")) as { version: string };
    return data.version;
  }

  async capabilities(): Promise<string[]> {
    const data = (await this.request("capabilities")) as { permissions: string[] };
    return data.permissions;
  }

  // --- Settings ---

  async setSetting(key: string, value: string): Promise<void> {
    await this.request("set_setting", { key, value });
  }

  async getSettings(): Promise<Record<string, string>> {
    const data = (await this.request("get_settings")) as { settings: Record<string, string> };
    return data.settings;
  }

  // --- Mention Groups ---

  async createMentionGroup(slug: string): Promise<number> {
    const data = (await this.request("mention_group_create", { slug })) as { id: number };
    return data.id;
  }

  async deleteMentionGroup(slug: string): Promise<void> {
    await this.request("mention_group_delete", { slug });
  }

  async getMentionGroup(slug: string): Promise<MentionGroup> {
    const data = (await this.request("mention_group_get", { slug })) as unknown;
    return toCamel<MentionGroup>(data);
  }

  async listMentionGroups(): Promise<MentionGroup[]> {
    const data = (await this.request("mention_group_list")) as { groups: unknown[] };
    return toCamelArray<MentionGroup>(data.groups);
  }

  async addMentionGroupMember(slug: string, username: string): Promise<void> {
    await this.request("mention_group_add_member", { slug, username });
  }

  async removeMentionGroupMember(slug: string, username: string): Promise<void> {
    await this.request("mention_group_remove_member", { slug, username });
  }
}
