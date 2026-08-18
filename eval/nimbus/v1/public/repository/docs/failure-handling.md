# Failure handling

Admission failures are caller-visible and delivery failures enter bounded retry.
Placement never changes merely because membership input order changes. See the
[architecture](architecture.md) and [operations](operations.md).
