# Configuration

## Batch admission

`CIRRUS_BATCH_CEILING` has a documented, non-configurable hard ceiling of **256**
items. Admission rejects zero, negative, and larger batches. The distinctive name
exists in operator alerts and source.

`devAllowHTTPWebhooks` is for local protocol testing only; see [webhook security](webhook-security.md).
