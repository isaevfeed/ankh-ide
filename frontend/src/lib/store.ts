import { useSyncExternalStore } from "react";
import type { SearchOpts } from "./search";
import type { ACPInfo, AgentContext, DiffMode, Project, Session, StateDTO, Status, Usage, View } from "./types";

export interface Toast {
  id: number;
  text: string;
  action?: { label: string; run: () => void };
}

export interface UIState {
  st: StateDTO | null;
  status: Record<string, Status>;
  usage: Usage | null;
  usageMissing: boolean; // statusLine arrives without rate_limits
  views: Record<string, View[]>;
  activeView: Record<string, number>;
  showFiles: boolean;
  showChanges: boolean;
  toasts: Toast[];
  gitTick: number; // bumps when the active session's git state changes
  fileFocus: { sessionId: string; path: string } | null;
  widths: { files: number; changes: number };
  context: Record<string, AgentContext>; // per session, from statusLine
  clearHint: Record<string, string>; // per session: why a fresh conversation makes sense
  hintDismissed: Record<string, string>; // per session: which hint the user closed
  fsTick: number; // bumps when files of the active session change on disk
  fsDirs: string[] | null; // folders that changed with the last fsTick (null: unknown)
  quickOpen: boolean;
  sidebar: "files" | "search";
  searchFocus: number; // bump to focus the project search input
  /** Open a file at a line; with query the preview's find bar opens with it. */
  jump: { sessionId: string; path: string; line: number; query?: string; opts?: SearchOpts; seq: number } | null;
  acp: Record<string, ACPInfo>; // chat (ACP) per session
}

function initialWidths(): { files: number; changes: number } {
  try {
    const v = JSON.parse(localStorage.getItem("ankh.widths") ?? "{}") as { files?: number; changes?: number };
    return { files: v.files ?? 240, changes: v.changes ?? 440 };
  } catch {
    return { files: 240, changes: 440 };
  }
}

let state: UIState = {
  st: null,
  status: {},
  usage: null,
  usageMissing: false,
  views: {},
  activeView: {},
  showFiles: true,
  showChanges: true,
  toasts: [],
  gitTick: 0,
  fileFocus: null,
  widths: initialWidths(),
  context: {},
  clearHint: {},
  hintDismissed: {},
  fsTick: 0,
  fsDirs: null,
  quickOpen: false,
  sidebar: "files",
  searchFocus: 0,
  jump: null,
  acp: {},
};

const listeners = new Set<() => void>();

export function getState(): UIState {
  return state;
}

export function setState(fn: (s: UIState) => Partial<UIState>): void {
  state = { ...state, ...fn(state) };
  listeners.forEach((l) => l());
}

export function useUI<T>(sel: (s: UIState) => T): T {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => sel(state),
  );
}

export function activeProject(s: UIState): Project | null {
  const ps = s.st?.data.projects ?? [];
  return ps.find((p) => p.id === s.st?.data.activeProject) ?? ps[0] ?? null;
}

export function activeSession(s: UIState): Session | null {
  const p = activeProject(s);
  if (!p) return null;
  return p.sessions.find((x) => x.id === p.activeSession) ?? p.sessions[0] ?? null;
}

export function findSession(s: UIState, id: string): { project: Project; session: Session } | null {
  for (const p of s.st?.data.projects ?? []) {
    const session = p.sessions.find((x) => x.id === id);
    if (session) return { project: p, session };
  }
  return null;
}

const defaultViews: View[] = [{ type: "agent" }];
// Stable empty values: selectors must not return fresh objects on every call.
export const EMPTY: never[] = [];

export function viewsOf(s: UIState, sessionId: string): View[] {
  return s.views[sessionId] ?? defaultViews;
}

export function diffMode(s: UIState): DiffMode {
  return s.st?.data.settings.diffMode ?? "wip";
}

let toastSeq = 0;
export function toast(text: string, action?: Toast["action"]): void {
  const id = ++toastSeq;
  setState((s) => ({ toasts: [...s.toasts, { id, text, action }].slice(-4) }));
  setTimeout(() => setState((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })), 5000);
}

export function projectStatus(s: UIState, p: Project): Status {
  const st = p.sessions.map((x) => s.status[x.id] ?? "idle");
  if (st.includes("waiting")) return "waiting";
  if (st.includes("working")) return "working";
  return "idle";
}
