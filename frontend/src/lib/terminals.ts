// Keeps one xterm.js instance per terminal id alive across React re-renders
// and tab switches. The DOM node is moved between containers, never recreated.
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { api, on } from "./api";

export interface TermEntry {
  id: string;
  term: Terminal;
  fit: FitAddon;
  el: HTMLDivElement;
  opened: boolean;
  exited: number | null;
  listeners: Set<() => void>;
}

const entries = new Map<string, TermEntry>();
const pending = new Map<string, Uint8Array[]>();

function decode(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function cssVar(name: string, fallback: string): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || fallback;
}

export function themeFromCSS() {
  return {
    background: cssVar("--term-bg", "#ffffff"),
    foreground: cssVar("--fg", "#15171c"),
    cursor: cssVar("--fg", "#15171c"),
    cursorAccent: cssVar("--term-bg", "#ffffff"),
    selectionBackground: cssVar("--term-sel", "rgba(47,91,211,.25)"),
  };
}

let wired = false;
function wire() {
  if (wired) return;
  wired = true;
  on<{ id: string; data: string }>("term:data", ({ id, data }) => {
    const bytes = decode(data);
    const e = entries.get(id);
    if (e) e.term.write(bytes);
    else pending.set(id, [...(pending.get(id) ?? []), bytes].slice(-200));
  });
  on<{ id: string; code: number }>("term:exit", ({ id, code }) => {
    const e = entries.get(id);
    if (!e) return;
    e.exited = code;
    e.listeners.forEach((l) => l());
  });
  on<{ id: string }>("term:restarted", ({ id }) => {
    const e = entries.get(id);
    if (!e) return;
    e.exited = null;
    e.term.write("\r\n\x1b[2mНечего продолжать — начинаю новый разговор.\x1b[0m\r\n");
    e.listeners.forEach((l) => l());
  });
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    setTimeout(() => entries.forEach((e) => (e.term.options.theme = themeFromCSS())), 50);
  });
}

export function getTerm(id: string): TermEntry {
  wire();
  let e = entries.get(id);
  if (e) return e;
  const term = new Terminal({
    fontFamily: '"SF Mono", ui-monospace, Menlo, Monaco, monospace',
    fontSize: 12.5,
    lineHeight: 1.25,
    cursorBlink: true,
    allowProposedApi: true,
    macOptionIsMeta: true,
    macOptionClickForcesSelection: true,
    scrollback: 20000,
    theme: themeFromCSS(),
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  const el = document.createElement("div");
  el.className = "term-host";
  e = { id, term, fit, el, opened: false, exited: null, listeners: new Set() };
  entries.set(id, e);

  // Let app shortcuts (⌘B, ⌘1…9, ⌘W, ⌘,) pass through the terminal.
  term.attachCustomKeyEventHandler((ev) => {
    if (ev.type !== "keydown" || !ev.metaKey) return true;
    const k = ev.key.toLowerCase();
    return !(ev.code === "KeyB" || /^[1-9]$/.test(k) || k === "," || k === "w" || k === "t");
  });
  term.onData((d) => {
    if (entries.get(id)?.exited !== null) return;
    void api.termWrite(id, d).catch(() => {});
  });
  term.onResize(({ cols, rows }) => void api.termResize(id, cols, rows).catch(() => {}));
  for (const chunk of pending.get(id) ?? []) term.write(chunk);
  pending.delete(id);
  return e;
}

/** Puts the terminal into a container and sizes it. */
export function attach(e: TermEntry, container: HTMLElement): void {
  // A container shows exactly one terminal: take out whatever was there
  // (another session's terminal) before putting this one in.
  for (const child of Array.from(container.children)) {
    if (child !== e.el) child.remove();
  }
  if (e.el.parentElement !== container) container.appendChild(e.el);
  if (!e.opened) {
    e.term.open(e.el);
    e.opened = true;
    try {
      e.term.loadAddon(new Unicode11Addon());
      e.term.unicode.activeVersion = "11";
    } catch {
      /* optional */
    }
    try {
      const gl = new WebglAddon();
      gl.onContextLoss(() => gl.dispose());
      e.term.loadAddon(gl);
    } catch {
      /* falls back to the DOM renderer */
    }
  }
  fitNow(e);
}

export function fitNow(e: TermEntry): void {
  if (!e.opened || !e.el.isConnected || e.el.clientWidth === 0) return;
  try {
    e.fit.fit();
  } catch {
    /* not visible yet */
  }
}

export function markRestarted(id: string): void {
  const e = entries.get(id);
  if (!e) return;
  e.exited = null;
  e.term.write("\r\n");
  e.listeners.forEach((l) => l());
}

export function disposeTerm(id: string): void {
  const e = entries.get(id);
  if (!e) return;
  e.term.dispose();
  e.el.remove();
  entries.delete(id);
}
