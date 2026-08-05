# Complete-replacement snapshots on every presence wire

All presence wires — Harness Adapter to Edge Bridge, Edge Bridge to Home
Bridge, and Edge/Home to device Presence Feeds — carry complete absolute
snapshots ordered by monotonic generations, producer sequences, or origin
revisions. Reconnects resend full current state; there is no event log and no
replay of raw lifecycle events anywhere in the system.

We chose this over event-sourced updates because convergence after any gap
(dropped frames, restarts, or a redundant direct-plus-relayed copy of the same
Task Presence) is trivially correct: newest snapshot wins, per pairing. It also
keeps no history that could leak or need retention rules. The cost — bandwidth
on state churn — is bounded because the Task Presence model is small and
adapters coalesce rapid updates.
