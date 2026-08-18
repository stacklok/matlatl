# Stable tenant placement

## Algorithm contract

For every tenant-node pair, hash `tenant`, one zero separator byte, and `node`
with 64-bit FNV-1a. Select the node with the greatest unsigned score; break equal
scores by lexicographically smaller node name. This rendezvous-like choice is
independent of input order and remaps only tenants affected by membership change.
A modulo of one tenant hash is not stable placement and is forbidden.

Return to the [scheduler](scheduler.md).
