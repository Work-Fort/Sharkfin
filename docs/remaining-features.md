# Remaining Features

Tracking document for planned sharkfin features.

## 1. wait_for_messages and Event Bus

[Design](2026-03-10-wait-for-messages-design.md) |
[Plan](plans/2026-03-10-wait-for-messages.md)

Add a domain-level EventBus for decoupled notification delivery. Refactor
webhook firing out of the hub into an EventBus subscriber. Add presence
WebSocket notifications. Implement `wait_for_messages` MCP tool in the
bridge that blocks until unread messages arrive (or timeout), using the
presence notification channel.
