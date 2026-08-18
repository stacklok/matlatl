# Cirrus Relay contributor notes

Run `go test ./...` before submitting a change. Preserve documented security and
lifecycle invariants; tests are not a substitute for the linked design records.
Use only the Go standard library. Do not weaken URL checks or placement stability.
