// Drop-in for the parts of @wailsio/runtime that Ankh uses, talking to
// cmd/devserver over HTTP. Selected at build time with `npm run build:http`.
type Handler = (e: { name: string; data: unknown }) => void;
const handlers = new Map<string, Set<Handler>>();
let source: EventSource | null = null;

function ensureSource() {
  if (source) return;
  source = new EventSource("/events");
  source.onmessage = (m) => {
    const ev = JSON.parse(m.data) as { name: string; data: unknown };
    handlers.get(ev.name)?.forEach((h) => h(ev));
  };
}

export const Call = {
  async ByName(fqn: string, ...args: unknown[]): Promise<unknown> {
    const method = fqn.split(".").pop()!;
    const r = await fetch("/call", { method: "POST", body: JSON.stringify({ method, args }) });
    const j = (await r.json()) as { result?: unknown; error?: string };
    if (j.error) throw new Error(j.error);
    return j.result ?? null;
  },
};

export const Events = {
  On(name: string, cb: Handler): () => void {
    ensureSource();
    if (!handlers.has(name)) handlers.set(name, new Set());
    handlers.get(name)!.add(cb);
    return () => handlers.get(name)?.delete(cb);
  },
};
