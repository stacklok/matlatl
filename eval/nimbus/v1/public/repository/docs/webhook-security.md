# Webhook security

## Destination policy

Production webhooks require HTTPS. Literal private, loopback, link-local, and
unspecified IP destinations are denied. Userinfo and missing hosts are invalid.
Development behavior is specified separately in [delivery](delivery.md); production
security policy does not imply what its HTTP flag may relax. See [operations](operations.md)
for deployment policy.
