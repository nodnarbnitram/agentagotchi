// HomeDurableObject: exactly one instance per Home. Holds presence state,
// pairing state, credentials, and all Edge/device WebSocket connections;
// Durable Object storage (SQLite) is the persistence layer; alarms drive
// time-based checks; WebSocket hibernation keeps idle long-lived connections
// open across runtime eviction.
//
// The Home is a relay, never the task authority: no Harness Adapter IPC, no
// App Server process, no desktop focus, no local-machine authority.

import { HomePresence } from "./presence.ts";
import { PairingCeremony, type PairingRole } from "./pairing.ts";
import {
  SCHEMA_ADMIN,
  SCHEMA_FEED,
  SCHEMA_PAIRING,
  SCHEMA_UPSTREAM,
  validateUpstreamSnapshot,
  type ActionResult,
  type FeedAction,
  type UpstreamActionRequest,
} from "./wire.ts";

export interface Env {
  // Bound per deployment; the admin secret is set via wrangler secret.
  ADMIN_BOOTSTRAP_TOKEN?: string;
}

interface SessionRecord {
  token: string;
  createdAt: string;
  csrf: string;
}

const SESSION_COOKIE = "agot_admin";
const SESSION_TTL_MS = 12 * 60 * 60 * 1000;
const CODE_TTL_REDEEM_WINDOW_MS = 60 * 1000;

export class HomeDurableObject {
  private presence: HomePresence;
  private ceremony: PairingCeremony;
  private homeId: string;
  private adminPasswordHash: string | null = null;
  private sessions = new Map<string, SessionRecord>();
  private loaded = false;

  private readonly ctx: DurableObjectState;
  private readonly env: Env;

  constructor(ctx: DurableObjectState, env: Env) {
    this.ctx = ctx;
    this.env = env;
    this.homeId = ctx.id.toString();
    this.presence = new HomePresence(this.homeId);
    this.ceremony = new PairingCeremony();
  }

  private async ensureLoaded(): Promise<void> {
    if (this.loaded) return;
    const stored = await this.ctx.storage.get<{
      ceremony?: ReturnType<PairingCeremony["dump"]>;
      adminPasswordHash?: string;
      presence?: ReturnType<HomePresence["dump"]>;
      sessions?: SessionRecord[];
    }>("home");
    if (stored?.ceremony) {
      this.ceremony = PairingCeremony.load(stored.ceremony);
    }
    if (stored?.adminPasswordHash) {
      this.adminPasswordHash = stored.adminPasswordHash;
    }
    this.presence = HomePresence.load(this.homeId, stored?.presence);
    for (const session of stored?.sessions ?? []) {
      if (Date.now() - Date.parse(session.createdAt) <= SESSION_TTL_MS) {
        this.sessions.set(session.token, session);
      }
    }
    this.loaded = true;
  }

  // Persistence covers the full one-Home state per spec 5.2: privacy-safe
  // presence model, pairing state, admin credentials, and sessions.
  private async persist(): Promise<void> {
    await this.ctx.storage.put("home", {
      ceremony: this.ceremony.dump(),
      adminPasswordHash: this.adminPasswordHash ?? undefined,
      presence: this.presence.dump(),
      sessions: [...this.sessions.values()],
    });
  }

  async fetch(request: Request): Promise<Response> {
    await this.ensureLoaded();
    const url = new URL(request.url);
    switch (url.pathname) {
      case "/edge/v1":
        return this.handleEdgeIngress(request);
      case "/feed/v1":
        return this.handleDeviceFeed(request);
      case "/pairing/code":
        return this.handlePairingCode(request);
      case "/pairing/redeem":
        return this.handlePairingRedeem(request);
      case "/admin/bootstrap":
        return this.handleAdminBootstrap(request);
      case "/admin/login":
        return this.handleAdminLogin(request);
      default:
        if (url.pathname.startsWith("/admin/api/")) {
          return this.handleAdminApi(request, url.pathname.slice("/admin/api/".length));
        }
        return new Response("not found", { status: 404 });
    }
  }

  // --- Edge ingress -------------------------------------------------------

  private async handleEdgeIngress(request: Request): Promise<Response> {
    const cred = this.bearerCredential(request);
    if (cred === null || cred.role !== "edge-ingress") {
      return new Response("unauthorized", { status: 401 });
    }
    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);
    // The credential authenticates the connection; the Edge's stable identity
    // is the snapshot's edgeId (cert-derived at the Edge). The contribution is
    // keyed by edgeId once the first valid snapshot arrives; until then the
    // socket is tracked under a per-connection label for revocation sweeps.
    const connectionId = crypto.randomUUID();
    this.ctx.acceptWebSocket(server, ["edge", connectionId]);
    server.serializeAttachment({ kind: "edge", edgeId: "", connectionId, token: cred.token });
    return new Response(null, { status: 101, webSocket: client });
  }

  async webSocketMessage(ws: WebSocket, message: string | ArrayBuffer): Promise<void> {
    await this.ensureLoaded();
    const attachment = ws.deserializeAttachment() as
      | { kind: "edge"; edgeId: string; token: string }
      | { kind: "device"; token: string }
      | null;
    if (attachment === null || typeof message !== "string") {
      ws.close(1003, "unsupported");
      return;
    }
    if (attachment.kind === "edge") {
      await this.handleEdgeMessage(ws, attachment, message);
    } else {
      await this.handleDeviceMessage(ws, attachment, message);
    }
  }

  async webSocketClose(ws: WebSocket): Promise<void> {
    const attachment = ws.deserializeAttachment() as
      | { kind: "edge"; edgeId: string }
      | { kind: "device" }
      | null;
    if (attachment?.kind === "edge") {
      if (attachment.edgeId !== "" && this.presence.removeEdge(attachment.edgeId)) {
        await this.persist();
        this.broadcastFeed();
      }
    }
  }

  private async handleEdgeMessage(
    ws: WebSocket,
    attachment: { edgeId: string },
    message: string,
  ): Promise<void> {
    let frame: unknown;
    try {
      frame = JSON.parse(message);
    } catch {
      ws.close(1003, "invalid JSON");
      return;
    }
    const envelope = frame as { schema?: string; type?: string };
    if (envelope.schema !== SCHEMA_UPSTREAM) {
      ws.close(1003, "wrong schema");
      return;
    }
    if (envelope.type === "snapshot") {
      const snapshot = validateUpstreamSnapshot(frame);
      if (snapshot === null) {
        ws.close(1003, "invalid snapshot");
        return;
      }
      if (attachment.edgeId === "") {
        // First valid snapshot binds the Edge's stable identity to this
        // connection (and re-tags for hibernation-safe lookups).
        attachment.edgeId = snapshot.edgeId;
        ws.serializeAttachment(attachment);
      } else if (snapshot.edgeId !== attachment.edgeId) {
        // One connection, one Edge identity — a mid-stream identity change is
        // a protocol violation; fail closed.
        ws.close(1003, "edge identity changed");
        return;
      }
      if (this.presence.applySnapshot(snapshot)) {
        await this.persist();
        this.broadcastFeed();
      }
      return;
    }
    if (envelope.type === "action_result") {
      const result = frame as ActionResult & { schema: string };
      const pending = this.pendingActions.get(result.actionId);
      if (pending !== undefined) {
        this.pendingActions.delete(result.actionId);
        // Re-tag to the feed schema: the device speaks agentagotchi.feed.v1
        // and must never see the upstream schema.
        this.sendJson(pending, {
          schema: SCHEMA_FEED,
          type: "action_result",
          actionId: result.actionId,
          status: result.status,
        } satisfies ActionResult);
      }
      return;
    }
    ws.close(1003, "unknown frame");
  }

  // --- Device feeds -------------------------------------------------------

  private async handleDeviceFeed(request: Request): Promise<Response> {
    const cred = this.bearerCredential(request);
    if (cred === null || cred.role !== "feed") {
      return new Response("unauthorized", { status: 401 });
    }
    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);
    this.ctx.acceptWebSocket(server, ["device"]);
    server.serializeAttachment({ kind: "device", token: cred.token });
    // Send the current snapshot immediately after accept.
    this.sendJson(server, this.presence.feedSnapshot());
    return new Response(null, { status: 101, webSocket: client });
  }

  private pendingActions = new Map<string, WebSocket>();

  private async handleDeviceMessage(
    ws: WebSocket,
    _attachment: { token: string },
    message: string,
  ): Promise<void> {
    let action: FeedAction;
    try {
      action = JSON.parse(message) as FeedAction;
    } catch {
      return;
    }
    const reply = (status: ActionResult["status"]) =>
      this.sendJson(ws, {
        schema: SCHEMA_FEED,
        type: "action_result",
        actionId: action.actionId ?? "",
        status,
      } satisfies ActionResult);
    if (
      action.schema !== SCHEMA_FEED ||
      action.type !== "action" ||
      typeof action.actionId !== "string" ||
      action.actionId === ""
    ) {
      return reply("failed");
    }
    // Fail-closed validation: capability advertised, task currently owned by
    // an authenticated Edge connection. Dismissal actions (acknowledge/snooze)
    // are Edge-global controls — never advertised, always forwarded to the
    // owning Edge, which enforces target-state rules.
    const DISMISSAL = new Set(["acknowledge", "snooze"]);
    const capabilities = this.presence.capabilitiesOf(action.taskPresenceId);
    if (capabilities === undefined) return reply("stale");
    if (!DISMISSAL.has(action.capability) && !capabilities.includes(action.capability)) {
      return reply("unsupported");
    }
    // The device's seenRevision refers to the Home's merged feed revision —
    // the only revision a device can see (per-task origin revisions are
    // stripped from the privacy-safe feed projection). Fail fast if the
    // device's view is stale, then translate to the owning Edge's last-known
    // revision, which is the sequence the Edge validates against. A
    // concurrent Edge-side change still fails closed at the Edge; the fresh
    // snapshot that follows lets the device retry (actions are never queued).
    if (action.seenRevision !== this.presence.revision()) return reply("stale");
    const ownerEdgeId = this.presence.ownerOf(action.taskPresenceId);
    if (ownerEdgeId === undefined) return reply("stale");
    const originRevision = this.presence.originRevisionOf(ownerEdgeId);
    if (originRevision === undefined) return reply("stale");
    const edgeSocket = this.findEdgeSocket(ownerEdgeId);
    if (edgeSocket === null) {
      // Never reroute, never invent, never queue.
      return reply("unavailable");
    }
    const request: UpstreamActionRequest = {
      schema: SCHEMA_UPSTREAM,
      type: "action_request",
      actionId: action.actionId,
      capability: action.capability,
      taskPresenceId: action.taskPresenceId,
      seenRevision: originRevision,
    };
    this.pendingActions.set(action.actionId, ws);
    this.sendJson(edgeSocket, request);
  }

  private findEdgeSocket(edgeId: string): WebSocket | null {
    for (const ws of this.ctx.getWebSockets("edge")) {
      const attachment = ws.deserializeAttachment() as { edgeId?: string } | null;
      if (attachment?.edgeId === edgeId) return ws;
    }
    return null;
  }

  private broadcastFeed(): void {
    const snapshot = this.presence.feedSnapshot();
    for (const ws of this.ctx.getWebSockets("device")) {
      this.sendJson(ws, snapshot);
    }
  }

  private sendJson(ws: WebSocket, value: unknown): void {
    try {
      ws.send(JSON.stringify(value));
    } catch {
      // Connection gone; hibernation/close handling cleans up.
    }
  }

  // --- Pairing ceremony (HTTP for connecting clients) ---------------------

  private async handlePairingCode(request: Request): Promise<Response> {
    if (request.method !== "POST") return new Response("method", { status: 405 });
    const body = (await request.json()) as { role?: PairingRole; clientName?: string };
    if (body.role === undefined || body.clientName === undefined) {
      return new Response("bad request", { status: 400 });
    }
    try {
      const code = this.ceremony.requestCode(body.role, body.clientName);
      await this.persist();
      return Response.json({
        schema: SCHEMA_PAIRING,
        type: "code",
        code: { id: code.id, token: code.token, expiresAt: code.expiresAt },
      });
    } catch {
      return new Response("unavailable", { status: 429 });
    }
  }

  private async handlePairingRedeem(request: Request): Promise<Response> {
    if (request.method !== "POST") return new Response("method", { status: 405 });
    const body = (await request.json()) as { codeToken?: string };
    if (body.codeToken === undefined) return new Response("bad request", { status: 400 });
    const cred = this.ceremony.redeem(body.codeToken);
    if (cred === null) {
      await this.persist();
      return new Response("not approved or unknown", { status: 403 });
    }
    await this.persist();
    return Response.json({
      schema: SCHEMA_PAIRING,
      type: "credential",
      credential: { id: cred.id, token: cred.token, role: cred.role },
    });
  }

  // --- Administration -------------------------------------------------------

  private async handleAdminBootstrap(request: Request): Promise<Response> {
    if (request.method !== "POST") return new Response("method", { status: 405 });
    if (this.adminPasswordHash !== null) {
      return new Response("already bootstrapped", { status: 409 });
    }
    const bootstrapToken = request.headers.get("x-bootstrap-token") ?? "";
    if (
      this.env.ADMIN_BOOTSTRAP_TOKEN === undefined ||
      bootstrapToken !== this.env.ADMIN_BOOTSTRAP_TOKEN
    ) {
      return new Response("unauthorized", { status: 401 });
    }
    const body = (await request.json()) as { password?: string };
    if (typeof body.password !== "string" || body.password.length < 12) {
      return new Response("password must be >= 12 chars", { status: 400 });
    }
    this.adminPasswordHash = await hashPassword(body.password);
    await this.persist();
    return this.issueSession();
  }

  private async handleAdminLogin(request: Request): Promise<Response> {
    if (request.method !== "POST") return new Response("method", { status: 405 });
    if (this.adminPasswordHash === null) {
      return new Response("not bootstrapped", { status: 409 });
    }
    const body = (await request.json()) as { password?: string };
    if (typeof body.password !== "string") return new Response("bad request", { status: 400 });
    const candidate = await hashPassword(body.password);
    if (candidate !== this.adminPasswordHash) {
      return new Response("unauthorized", { status: 401 });
    }
    return this.issueSession();
  }

  private issueSession(): Response {
    const token = crypto.randomUUID();
    const csrf = crypto.randomUUID();
    this.sessions.set(token, {
      token, csrf, createdAt: new Date().toISOString(),
    });
    // Persist so an idle-evicted Durable Object does not log the admin out.
    this.ctx.waitUntil(this.persist());
    const headers = new Headers({
      "set-cookie": `${SESSION_COOKIE}=${token}; HttpOnly; Secure; SameSite=Strict; Path=/`,
      "x-csrf-token": csrf,
    });
    return new Response(JSON.stringify({ ok: true, csrf }), { headers });
  }

  private adminSession(request: Request): SessionRecord | null {
    const cookie = request.headers.get("cookie") ?? "";
    const match = cookie.match(new RegExp(`(?:^|;\\s*)${SESSION_COOKIE}=([^;]+)`));
    if (match === null) return null;
    const session = this.sessions.get(match[1]);
    if (session === null || session === undefined) return null;
    if (Date.now() - Date.parse(session.createdAt) > SESSION_TTL_MS) {
      this.sessions.delete(session.token);
      return null;
    }
    return session;
  }

  private async handleAdminApi(request: Request, path: string): Promise<Response> {
    // Pairing credentials are never admin credentials: admin auth is cookie
    // sessions only, plus CSRF token on state changes.
    const session = this.adminSession(request);
    if (session === null) return new Response("unauthorized", { status: 401 });
    if (request.method !== "GET") {
      const csrf = request.headers.get("x-csrf-token");
      if (csrf !== session.csrf) return new Response("forbidden", { status: 403 });
    }
    switch (path) {
      case "status":
        return Response.json({
          schema: SCHEMA_ADMIN,
          type: "status",
          role: "home",
          homeId: this.homeId,
          edges: this.presence.edgeIds(),
          devices: this.ctx.getWebSockets("device").length,
          taskPresences: this.presence.mergedTasks().length,
          snapshot: this.presence.feedSnapshot(),
        });
      case "pairing/pending":
        return Response.json({ pending: this.ceremony.pending() });
      case "pairing/list":
        return Response.json({ credentials: this.ceremony.list() });
      case "pairing/approve": {
        const body = (await request.json()) as { codeId?: string };
        const ok = body.codeId !== undefined && this.ceremony.approve(body.codeId);
        await this.persist();
        return Response.json({ ok });
      }
      case "pairing/deny": {
        const body = (await request.json()) as { codeId?: string };
        const ok = body.codeId !== undefined && this.ceremony.deny(body.codeId);
        await this.persist();
        return Response.json({ ok });
      }
      case "pairing/revoke": {
        const body = (await request.json()) as { credentialId?: string };
        if (body.credentialId === undefined) return Response.json({ ok: false });
        const token = this.ceremony.tokenOf(body.credentialId);
        const ok = this.ceremony.revoke(body.credentialId);
        if (ok && token !== undefined) {
          // Disconnect every live connection presenting the revoked token.
          for (const tag of ["edge", "device"]) {
            for (const ws of this.ctx.getWebSockets(tag)) {
              const attachment = ws.deserializeAttachment() as { token?: string } | null;
              if (attachment?.token === token) ws.close(1008, "revoked");
            }
          }
        }
        await this.persist();
        return Response.json({ ok });
      }
      default:
        return new Response("not found", { status: 404 });
    }
  }

  private bearerCredential(request: Request) {
    const header = request.headers.get("authorization") ?? "";
    const token = header.toLowerCase().startsWith("bearer ")
      ? header.slice(7).trim()
      : "";
    if (token === "") return null;
    return this.ceremony.authenticate(token);
  }

  // --- Alarms: time-based checks ------------------------------------------

  async alarm(): Promise<void> {
    await this.ensureLoaded();
    // Session expiry sweep; pairing code expiry is lazy on access.
    const now = Date.now();
    for (const [token, session] of this.sessions) {
      if (now - Date.parse(session.createdAt) > SESSION_TTL_MS + CODE_TTL_REDEEM_WINDOW_MS) {
        this.sessions.delete(token);
      }
    }
  }
}

async function hashPassword(password: string): Promise<string> {
  const data = new TextEncoder().encode(`agentagotchi-home:${password}`);
  const digest = await crypto.subtle.digest("SHA-256", data);
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}
