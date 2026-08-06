# Harness-native session IDs never leave the owning Edge

Each Edge Bridge assigns an opaque canonical UUID (the Task Presence ID) to
every Task Presence it owns and keeps the mapping to
`{adapter, native session id, capabilities}` private and local. Edge-to-Home
and device wires carry no harness type, machine identity, hostname, username,
or path as independent fields; schema identification stays fail-closed.

We chose this over carrying harness-native identifiers upstream for
debuggability because the privacy boundary is the product's core promise, and
because a single opaque identity keeps the semantic core free of per-harness
vocabulary. The cost is operational: correlating a Task Presence back to a
harness session requires owner-only tooling on the owning Edge.
