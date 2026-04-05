// SPDX-License-Identifier: Apache-2.0

/** User from user_list response. */
export interface User {
  username: string;
  online: boolean;
  type: string;
  state?: string;
}

/** Channel from channel_list response. */
export interface Channel {
  name: string;
  public: boolean;
  member: boolean;
}

/** Message from history or unread_messages response. Wire format uses snake_case. */
export interface Message {
  id?: number;
  channel?: string;
  from: string;
  body: string;
  sentAt: string;
  threadId?: number;
  mentions?: string[];
}

/** Payload of a message.new broadcast. */
export interface BroadcastMessage {
  id: number;
  channel: string;
  channelType: string;
  from: string;
  body: string;
  sentAt: string;
  threadId?: number;
  mentions?: string[];
}

/** Payload of a presence broadcast. */
export interface PresenceUpdate {
  username: string;
  status: "online" | "offline";
  state?: "active" | "idle";
}

/** One entry in unread_counts response. */
export interface UnreadCount {
  channel: string;
  type: string;
  unreadCount: number;
  mentionCount: number;
}

/** One entry in dm_list response. */
export interface DM {
  channel: string;
  participants: string[];
}

/** Result of dm_open. */
export interface DMOpenResult {
  channel: string;
  participant: string;
  created: boolean;
}

/** Mention group. */
export interface MentionGroup {
  id: number;
  slug: string;
  createdBy?: string;
  members?: string[];
}

/** Options for sendMessage. */
export interface SendOptions {
  threadId?: number;
}

/** Options for history. */
export interface HistoryOptions {
  before?: number;
  after?: number;
  limit?: number;
  threadId?: number;
}

/** Options for unreadMessages. */
export interface UnreadOptions {
  mentionsOnly?: boolean;
  threadId?: number;
}

/** Client constructor options. */
export interface ClientOptions {
  token?: string;
  apiKey?: string;
  reconnect?: boolean | ((attempt: number) => number);
  logger?: Pick<Console, "log" | "warn" | "error">;
  WebSocket?: typeof WebSocket;
}

/** Wire envelope (internal). */
export interface Envelope {
  type: string;
  d?: unknown;
  ref?: string;
  ok?: boolean;
}
