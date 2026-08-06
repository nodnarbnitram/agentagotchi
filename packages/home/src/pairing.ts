// Pairing Ceremony for the Home Bridge: the same device-code state machine
// as the Edge (one-use short-lived codes, unique revocable role-scoped
// credentials), backed by Durable Object storage scoped to this Home.

export type PairingRole = "feed" | "edge-ingress";

export interface PairingCode {
  id: string;
  token: string;
  role: PairingRole;
  clientName: string;
  createdAt: string;
  expiresAt: string;
  consumed: boolean;
  approved: boolean;
}

export interface PairingCredential {
  id: string;
  token: string;
  role: PairingRole;
  clientName: string;
  issuedAt: string;
  revoked: boolean;
}

export const CODE_TTL_MS = 10 * 60 * 1000;
const MAX_PENDING = 32;

function randomHex(bytes: number): string {
  const buf = new Uint8Array(bytes);
  crypto.getRandomValues(buf);
  return [...buf].map((b) => b.toString(16).padStart(2, "0")).join("");
}

function constantTimeEqual(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) {
    diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  }
  return diff === 0;
}

export class PairingCeremony {
  private codes = new Map<string, PairingCode>();
  private credentials = new Map<string, PairingCredential>();
  private dirty = false;
  private readonly now: () => number;

  constructor(now: () => number = () => Date.now()) {
    this.now = now;
  }

  /** Restore from DO storage. */
  static load(data: { codes?: PairingCode[]; credentials?: PairingCredential[] }): PairingCeremony {
    const ceremony = new PairingCeremony();
    for (const code of data.codes ?? []) ceremony.codes.set(code.id, code);
    for (const cred of data.credentials ?? []) ceremony.credentials.set(cred.id, cred);
    ceremony.dirty = false;
    return ceremony;
  }

  /** Persist shape for DO storage (only pairing state — no other secrets). */
  dump(): { codes: PairingCode[]; credentials: PairingCredential[] } {
    return {
      codes: [...this.codes.values()],
      credentials: [...this.credentials.values()],
    };
  }

  takeDirty(): boolean {
    const was = this.dirty;
    this.dirty = false;
    return was;
  }

  private expire(): void {
    const now = this.now();
    for (const [id, code] of this.codes) {
      if (now > Date.parse(code.expiresAt)) this.codes.delete(id);
    }
  }

  requestCode(role: PairingRole, clientName: string): PairingCode {
    if (role !== "feed" && role !== "edge-ingress") {
      throw new Error("invalid pairing role");
    }
    this.expire();
    if (this.codes.size >= MAX_PENDING) throw new Error("too many pending codes");
    const now = new Date(this.now());
    const code: PairingCode = {
      id: randomHex(8),
      token: randomHex(16),
      role,
      clientName,
      createdAt: now.toISOString(),
      expiresAt: new Date(now.getTime() + CODE_TTL_MS).toISOString(),
      consumed: false,
      approved: false,
    };
    this.codes.set(code.id, code);
    this.dirty = true;
    return code;
  }

  approve(codeId: string): boolean {
    this.expire();
    const code = this.codes.get(codeId);
    if (code === undefined || code.consumed) return false;
    code.approved = true;
    this.dirty = true;
    return true;
  }

  deny(codeId: string): boolean {
    const deleted = this.codes.delete(codeId);
    if (deleted) this.dirty = true;
    return deleted;
  }

  pending(): PairingCode[] {
    this.expire();
    return [...this.codes.values()];
  }

  redeem(codeToken: string): PairingCredential | null {
    this.expire();
    let code: PairingCode | undefined;
    for (const candidate of this.codes.values()) {
      if (constantTimeEqual(candidate.token, codeToken)) {
        code = candidate;
        break;
      }
    }
    if (code === undefined) return null;
    if (code.consumed) return null;
    if (!code.approved) return null; // unapproved codes grant nothing
    code.consumed = true;
    this.codes.delete(code.id); // one-use, never replayable
    const cred: PairingCredential = {
      id: randomHex(8),
      token: randomHex(32),
      role: code.role,
      clientName: code.clientName,
      issuedAt: new Date(this.now()).toISOString(),
      revoked: false,
    };
    this.credentials.set(cred.id, cred);
    this.dirty = true;
    return cred;
  }

  authenticate(token: string): PairingCredential | null {
    for (const cred of this.credentials.values()) {
      if (!cred.revoked && constantTimeEqual(cred.token, token)) return cred;
    }
    return null;
  }

  revoke(credentialId: string): boolean {
    const cred = this.credentials.get(credentialId);
    if (cred === undefined) return false;
    cred.revoked = true;
    this.dirty = true;
    return true;
  }

  /** Administration listing: tokens are redacted — status carries no secrets. */
  list(): PairingCredential[] {
    return [...this.credentials.values()].map((cred) => ({ ...cred, token: "" }));
  }

  /** Token for disconnect sweeps on revocation (never serialized to status). */
  tokenOf(credentialId: string): string | undefined {
    return this.credentials.get(credentialId)?.token;
  }
}
