# Delivery

A worker validates the complete webhook URL immediately before dispatch. Delivery
uses the [security destination policy](webhook-security.md). The development flag
permits plain HTTP for protocol testing, but relaxes only the scheme and never
permits a private destination. Sessions are covered by [sessions](sessions.md).
