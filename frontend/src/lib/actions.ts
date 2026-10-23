import { api, errText, on } from "./api";
import { activeProject, activeSession, findSession, getState, setState, toast, viewsOf } from "./store";
import type { SearchOpts } from "./search";
import type { ACPEvent, ACPInfo, ACPState, AgentContext, Settings, Status, Usage, View } from "./types";

const lastBranch = new Map<string, string>();

export async function reload(): Promise<void> {
  try {
    const st = await api.getState();
    for (const p of st.data.projects ?? []) {
      for (const x of p.sessions) {
        const prev = lastBranch.get(x.id);
        if (prev && x.branch && prev !== x.branch) suggestClear(x.id, `ветка сменилась: ${prev} → ${x.branch}`);
        if (x.branch) lastBranch.set(x.id, x.branch);
      }
    }
    setState((s) => ({ st, status: { ...s.status, ...st.status }, usage: st.usage ?? s.usage }));
  } catch (e) {
    toast("Не удалось загрузить состояние: " + errText(e));
  }
}

export async function selectProject(projectId: string): Promise<void> {
  await guard(api.setActive(projectId, ""));
  await reload();
}

export async function selectSession(projectId: string, sessionId: string): Promise<void> {
  setState((s) => ({ status: { ...s.status, [sessionId]: s.status[sessionId] === "waiting" ? "idle" : s.status[sessionId] } }));
  await guard(api.setActive(projectId, sessionId));
  await reload();
}

export async function addProject(): Promise<void> {
  const p = await guard(api.addProject());
  if (p) await reload();
}

export async function guard<T>(p: Promise<T>): Promise<T | undefined> {
  try {
    return await p;
  } catch (e) {
    toast(errText(e));
    return undefined;
  }
}

export async function saveSettings(patch: Partial<Settings>): Promise<void> {
  const cur = getState().st?.data.settings;
  if (!cur) return;
  const next = { ...cur, ...patch };
  setState((s) => (s.st ? { st: { ...s.st, data: { ...s.st.data, settings: next } } } : {}));
  await guard(api.updateSettings(next));
}

export function setViews(sessionId: string, views: View[], active?: number): void {
  setState((s) => ({
    views: { ...s.views, [sessionId]: views },
    activeView: active === undefined ? s.activeView : { ...s.activeView, [sessionId]: active },
  }));
}

export function showAgent(sessionId: string): void {
  setState((s) => ({ activeView: { ...s.activeView, [sessionId]: 0 } }));
}

// Recently opened files per session, newest first (quick open ranks them up).
const recent = new Map<string, string[]>();

export function rememberOpened(sessionId: string, path: string): void {
  const r = (recent.get(sessionId) ?? []).filter((p) => p !== path);
  recent.set(sessionId, [path, ...r].slice(0, 20));
}

export function recentFiles(sessionId: string): string[] {
  return recent.get(sessionId) ?? [];
}

export function openFile(sessionId: string, path: string): void {
  rememberOpened(sessionId, path);
  const s = getState();
  const views = viewsOf(s, sessionId);
  const i = views.findIndex((v) => v.type === "file" && v.path === path);
  if (i < 0) {
    setViews(sessionId, [...views, { type: "file", path }], views.length);
  } else {
    setState((st) => ({ activeView: { ...st.activeView, [sessionId]: i } }));
  }
}

/** Is the session's chat (ACP) the view the user is looking at? */
export function chatActive(sessionId: string): boolean {
  const s = getState();
  const v = viewsOf(s, sessionId)[s.activeView[sessionId] ?? 0];
  const st = s.acp[sessionId]?.state;
  return v?.type === "chat" && (st === "ready" || st === "working");
}

let jumpSeq = 0;
/** Opens a file and scrolls to a line (from search results). */
export function openAt(sessionId: string, path: string, line: number, query?: string, opts?: SearchOpts): void {
  setState(() => ({ jump: { sessionId, path, line, query, opts, seq: ++jumpSeq } }));
  openFile(sessionId, path);
}

/** Sends (or, with autoSend off, only pastes) a prompt to the session's agent:
 *  to the chat when it is the open view, otherwise to the terminal agent. */
export async function deliver(sessionId: string, text: string): Promise<void> {
  if (chatActive(sessionId)) {
    await guard(api.acpPrompt(sessionId, text));
    return;
  }
  const submit = getState().st?.data.settings.autoSend ?? true;
  showAgent(sessionId);
  const ok = await guard(api.sendPrompt(sessionId, text, submit).then(() => true));
  if (ok && !submit) toast("Промпт вставлен в поле ввода агента");
}

export function fileRef(sessionId: string, path: string, from?: number, to?: number): string {
  const s = getState();
  const found = findSession(s, sessionId);
  const preset = s.st?.presets.find((p) => p.id === found?.session.agent) ?? s.st?.presets[0];
  let ref = (preset?.fileRef || "{path}").replace("{path}", path);
  if (from && to && to !== from) ref += `:${Math.min(from, to)}-${Math.max(from, to)}`;
  else if (from) ref += `:${from}`;
  return ref;
}

/** Wires backend events into the store. Call once. */
export function wireEvents(): void {
  on<{ session: string; status: Status }>("status", ({ session, status }) => {
    const s = getState();
    const act = activeSession(s);
    const visible = !document.hidden && act?.id === session;
    if (status === "waiting" && visible) {
      void api.markSeen(session);
      setState((st) => ({ status: { ...st.status, [session]: "idle" } }));
      return;
    }
    setState((st) => ({ status: { ...st.status, [session]: status } }));
    if (status === "waiting") {
      const f = findSession(s, session);
      const name = f ? `${f.project.name} · ${f.session.branch}` : "Агент";
      toast(`${name}: агент ждёт ввода`, f ? { label: "Открыть", run: () => void selectSession(f.project.id, session) } : undefined);
      if (document.hidden || !document.hasFocus()) void api.notify("Ankh", `${name}: агент ждёт ввода`);
    }
  });
  on<Usage>("usage", (u) => setState(() => ({ usage: u })));
  on<{ session: string; context: AgentContext }>("context", ({ session, context }) =>
    setState((s) => ({ context: { ...s.context, [session]: context } })),
  );
  on<boolean>("usage:missing", () => setState(() => ({ usageMissing: true })));
  on<string>("usage:error", (msg) => toast(msg));
  on<{ session: string }>("git:changed", () => setState((s) => ({ gitTick: s.gitTick + 1 })));
  on<{ session: string; dirs: string[] | null }>("fs:changed", ({ dirs }) => setState((s) => ({ fsTick: s.fsTick + 1, fsDirs: dirs ?? null })));
  on<{ id: string }>("term:restored", () => toast("Агент продолжил работу в tmux — сессия восстановлена"));
  on<{ session: string; state: ACPState; error: string }>("acp:state", ({ session, state, error }) =>
    setState((s) => ({ acp: { ...s.acp, [session]: { ...emptyChat(s.acp[session]), state, error } } })),
  );
  on<{ session: string; event: ACPEvent }>("acp:event", ({ session, event }) =>
    setState((s) => {
      const cur = emptyChat(s.acp[session]);
      return { acp: { ...s.acp, [session]: { ...cur, events: [...cur.events, event] } } };
    }),
  );
  window.addEventListener("focus", () => {
    const s = getState();
    const act = activeSession(s);
    if (act && s.status[act.id] === "waiting") {
      void api.markSeen(act.id);
      setState((st) => ({ status: { ...st.status, [act.id]: "idle" } }));
    }
    setState((st) => ({ gitTick: st.gitTick + 1 }));
  });
}

export function currentIds(): { projectId: string | null; sessionId: string | null } {
  const s = getState();
  return { projectId: activeProject(s)?.id ?? null, sessionId: activeSession(s)?.id ?? null };
}

/** Suggest starting a fresh conversation (/clear) in a session. */
export function suggestClear(sessionId: string, reason: string): void {
  setState((s) => ({ clearHint: { ...s.clearHint, [sessionId]: reason } }));
}

export function dismissHint(sessionId: string, key: string): void {
  setState((s) => ({
    hintDismissed: { ...s.hintDismissed, [sessionId]: key },
    clearHint: key.startsWith("clear") ? { ...s.clearHint, [sessionId]: "" } : s.clearHint,
  }));
}

export async function runAgentCommand(sessionId: string, cmd: "/compact" | "/clear"): Promise<void> {
  showAgent(sessionId);
  const ok = await guard(api.sendCommand(sessionId, cmd).then(() => true));
  if (!ok) return;
  setState((s) => ({
    clearHint: { ...s.clearHint, [sessionId]: "" },
    context: cmd === "/clear" ? Object.fromEntries(Object.entries(s.context).filter(([k]) => k !== sessionId)) : s.context,
    hintDismissed: { ...s.hintDismissed, [sessionId]: "" },
  }));
}

function emptyChat(c?: ACPInfo): ACPInfo {
  return c ?? { state: "", command: "", events: [] };
}

/** Opens the chat (ACP) view of a session and starts the agent in chat mode. */
export async function openChat(sessionId: string): Promise<void> {
  const vs = viewsOf(getState(), sessionId);
  const i = vs.findIndex((v) => v.type === "chat");
  if (i < 0) setViews(sessionId, [...vs, { type: "chat" }], vs.length);
  else setState((s) => ({ activeView: { ...s.activeView, [sessionId]: i } }));
  const info = await guard(api.acpState(sessionId));
  if (!info) return;
  setState((s) => ({ acp: { ...s.acp, [sessionId]: { ...info, events: info.events ?? [] } } }));
  if (info.state === "" ) await guard(api.acpStart(sessionId));
}
