// Auto-discovery entry point: pi finds extensions at
// ~/.pi/agent/extensions/<dir>/index.ts; the implementation lives in
// extension.ts and is referenced by package.json `pi.extensions` for
// npm/git package installs.
export { default } from "./extension.js";
