# Agentagotchi Home Bridge

The optional, always-reachable relay for one Home. Deployed as a Cloudflare
Worker with exactly one Durable Object per Home.

The Home:

- accepts outbound WSS from paired Edge Bridges (`/edge/v1`) carrying
  complete absolute snapshots (Edge generation + monotonic revision)
- replaces only that Edge's contribution; relays merged, privacy-safe
  presence to paired devices (`/feed/v1`)
- reverse-routes device actions to the owning Edge only — never reroutes,
  invents capabilities, or queues actions
- persists pairing state, credentials, and connectivity metadata in Durable
  Object storage (scoped to this Home)
- provides a single-admin browser client (static assets) with cookie
  sessions + CSRF protection

The Home is never the task authority and has no local harness capability: no
Harness Adapter IPC, no App Server process, no desktop focus.

## Privacy boundary

Only the allowlisted presence model crosses the Edge → Home → device path:
Task Presence IDs, Safe Titles, generic state/reason, allowlisted
capabilities, counts, ordering metadata, timestamps. Native session IDs,
prompts, commands, tool payloads, transcripts, and full paths never arrive
(fail-closed validation in `src/wire.ts`).

## Develop

```sh
npm install
npm test          # pure-module tests (presence merge, pairing, wire validation)
npm run dev       # wrangler dev (local Workers runtime)
npm run types     # tsc --noEmit
```

## Deploy (requires explicit authorization)

Deployment is an external write and runs only with explicit user
authorization:

```sh
wrangler secret put ADMIN_BOOTSTRAP_TOKEN   # one-time bootstrap secret
npm run deploy                              # wrangler deploy
```

After deploy, open the Worker URL, enter the bootstrap token + a new admin
password (≥ 12 chars) to create the single-admin session, then approve
pairing codes from Edges and devices.

## Pairing an Edge to this Home

On the Home admin UI: approve the pending `edge-ingress` code, then give the
issued credential to the Edge:

```sh
agentagotchi serve --home-url wss://<home-host>/edge/v1 --home-token <credential>
```
