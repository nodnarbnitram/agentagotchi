# Rebuild replaces the prototype with no runtime compatibility

The rebuild replaces the Codex-only prototype's protocol, credentials,
persistence, commands, and provisioning atomically. Git history is the rollback
path; there is no migration matrix, no parallel legacy state machine, and no
adapter preserving accidental prototype boundaries. `docs/PROTOCOL.md` is
replaced wholesale with the new role-separated contracts.

We chose a flag-day cutover because the prototype's boundaries were incidental
rather than designed, preserving them would fossilize mistakes into the new
core, and the entire install base is the author's own kit. The cost is a
one-time re-provisioning of devices under the new Pairing Ceremony and no mixed
old/new operation during the rebuild.
