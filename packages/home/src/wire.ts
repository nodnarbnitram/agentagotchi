// Wire contract types for the Agentagotchi Home Bridge.
// Mirror of docs/PROTOCOL.md; the Home is a relay, never the task authority.

export const SCHEMA_FEED = "agentagotchi.feed.v1";
export const SCHEMA_UPSTREAM = "agentagotchi.upstream.v1";
export const SCHEMA_PAIRING = "agentagotchi.pairing.v1";
export const SCHEMA_ADMIN = "agentagotchi.admin.v1";

export type PresenceState = "idle" | "running" | "needs_input" | "ready" | "blocked";
export type PresenceReason =
  | "working" | "question" | "approval" | "permission" | "completed" | "failed";

export const STATES: ReadonlySet<string> = new Set([
  "idle", "running", "needs_input", "ready", "blocked",
]);
export const REASONS: ReadonlySet<string> = new Set([
  "working", "question", "approval", "permission", "completed", "failed",
]);
export const CAPABILITIES: ReadonlySet<string> = new Set(["focus"]);

/** Allowlisted wire form of one Task Presence (privacy boundary). */
export interface FeedTask {
  taskPresenceId: string;
  safeTitle: string;
  state: PresenceState;
  reason: PresenceReason;
  subagentCount: number;
  capabilities: string[];
  updatedAt: string;
  snoozed: boolean;
}

export interface Counts {
  needsInput: number;
  blocked: number;
  ready: number;
  running: number;
}

export interface UpstreamSnapshot {
  schema: string;
  type: "snapshot";
  edgeId: string;
  generation: number;
  revision: number;
  snapshotGeneratedAt: string;
  tasks: FeedTask[];
  counts: Counts;
  aggregateState: PresenceState;
}

export interface FeedSnapshot {
  schema: string;
  type: "snapshot";
  origin: { kind: "home"; id: string; generation: number; revision: number };
  generatedAt: string;
  aggregateState: PresenceState;
  counts: Counts;
  tasks: FeedTask[];
}

export interface FeedAction {
  schema: string;
  type: "action";
  actionId: string;
  capability: string;
  taskPresenceId: string;
  seenRevision: number;
}

export interface UpstreamActionRequest {
  schema: string;
  type: "action_request";
  actionId: string;
  capability: string;
  taskPresenceId: string;
  seenRevision: number;
}

export interface ActionResult {
  schema: string;
  type: "action_result";
  actionId: string;
  status: "ok" | "stale" | "unsupported" | "failed" | "unavailable";
}

const MAX_SAFE_TITLE_BYTES = 64;
const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

/** Strictly validate a feed task against the allowlist (fail-closed). */
export function validateFeedTask(value: unknown): FeedTask | null {
  if (typeof value !== "object" || value === null) return null;
  const t = value as Record<string, unknown>;
  if (typeof t.taskPresenceId !== "string" || !UUID_RE.test(t.taskPresenceId)) return null;
  if (typeof t.safeTitle !== "string" || t.safeTitle.length > MAX_SAFE_TITLE_BYTES) return null;
  if (typeof t.state !== "string" || !STATES.has(t.state)) return null;
  if (typeof t.reason !== "string" || !REASONS.has(t.reason)) return null;
  if (typeof t.subagentCount !== "number" || t.subagentCount < 0) return null;
  if (!Array.isArray(t.capabilities)) return null;
  for (const cap of t.capabilities) {
    if (typeof cap !== "string" || !CAPABILITIES.has(cap)) return null;
  }
  if (typeof t.updatedAt !== "string") return null;
  if (typeof t.snoozed !== "boolean") return null;
  // Privacy: reject objects carrying fields beyond the allowlist.
  const allowed = new Set([
    "taskPresenceId", "safeTitle", "state", "reason",
    "subagentCount", "capabilities", "updatedAt", "snoozed",
  ]);
  for (const key of Object.keys(t)) {
    if (!allowed.has(key)) return null;
  }
  return t as unknown as FeedTask;
}

/** Strictly validate an upstream snapshot (fail-closed, allowlist only). */
export function validateUpstreamSnapshot(value: unknown): UpstreamSnapshot | null {
  if (typeof value !== "object" || value === null) return null;
  const s = value as Record<string, unknown>;
  if (s.schema !== SCHEMA_UPSTREAM || s.type !== "snapshot") return null;
  if (typeof s.edgeId !== "string" || s.edgeId === "") return null;
  if (typeof s.generation !== "number" || s.generation < 1) return null;
  if (typeof s.revision !== "number" || s.revision < 0) return null;
  if (typeof s.snapshotGeneratedAt !== "string") return null;
  if (typeof s.aggregateState !== "string" || !STATES.has(s.aggregateState)) return null;
  if (!Array.isArray(s.tasks)) return null;
  const tasks: FeedTask[] = [];
  for (const raw of s.tasks) {
    const task = validateFeedTask(raw);
    if (task === null) return null;
    tasks.push(task);
  }
  const counts = s.counts as Record<string, unknown> | undefined;
  if (typeof counts !== "object" || counts === null) return null;
  for (const key of ["needsInput", "blocked", "ready", "running"]) {
    if (typeof counts[key] !== "number" || (counts[key] as number) < 0) return null;
  }
  return { ...(s as unknown as UpstreamSnapshot), tasks };
}
