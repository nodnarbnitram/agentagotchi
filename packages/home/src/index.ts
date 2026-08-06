// Agentagotchi Home Bridge Worker entry. Every request routes to the single
// Home Durable Object (one DO per Home; the deployment hosts exactly one).
// Static assets (the browser admin client) are served from /public.

import { HomeDurableObject, type Env as DOEnv } from "./home-do.ts";

export { HomeDurableObject };

export interface Env extends DOEnv {
  HOME: DurableObjectNamespace;
  ASSETS: Fetcher;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    if (
      url.pathname.startsWith("/edge/") ||
      url.pathname.startsWith("/feed/") ||
      url.pathname.startsWith("/pairing/") ||
      url.pathname.startsWith("/admin/")
    ) {
      // Exactly one Home per deployment: a well-known DO name pins it.
      const id = env.HOME.idFromName("home");
      const stub = env.HOME.get(id);
      return stub.fetch(request);
    }
    return env.ASSETS.fetch(request);
  },
};
