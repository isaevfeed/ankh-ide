import { useState } from "react";
import { api, errText } from "../lib/api";
import { guard, openChat, reload, selectSession, setViews } from "../lib/actions";
import { getState, toast, useUI, viewsOf, EMPTY } from "../lib/store";
import { disposeTerm, getTerm, markRestarted } from "../lib/terminals";
import type { Project, Session } from "../lib/types";
import { Menu, type MenuItem } from "./Floating";
import { TerminalView } from "./TerminalView";
import { FilePreview } from "./FilePreview";
import { NewSessionPopover, CloseSessionPopover } from "./SessionPopovers";
import { AgentHints } from "./AgentHints";
import { ChatView } from "./ChatView";

export function Center(props: { project: Project; session: Session }) {
  const { project, session } = props;
  const sid = session.id;
  const status = useUI((s) => s.status);
  const presets = useUI((s) => s.st?.presets ?? EMPTY);
  const views = useUI((s) => viewsOf(s, sid));
  const active = useUI((s) => Math.min(s.activeView[sid] ?? 0, viewsOf(s, sid).length - 1));
  const [newAt, setNewAt] = useState<{ x: number; y: number } | null>(null);
  const [closeFor, setCloseFor] = useState<{ session: Session; x: number; y: number } | null>(null);
  const [agentMenu, setAgentMenu] = useState<{ x: number; y: number } | null>(null);
  const preset = presets.find((p) => p.id === session.agent) ?? presets[0];

  const addShell = async () => {
    const t = getTerm(`${sid}/agent`);
    const id = await guard(api.startShell(sid, t.term.cols, t.term.rows));
    if (!id) return;
    const vs = viewsOf(getState(), sid);
    const n = vs.filter((v) => v.type === "shell").length + 1;
    setViews(sid, [...vs, { type: "shell", termId: id, n }], vs.length);
  };

  const closeView = (i: number) => {
    const vs = viewsOf(getState(), sid);
    const v = vs[i];
    if (v.type === "shell") {
      void api.termKill(v.termId);
      disposeTerm(v.termId);
    }
    if (v.type === "chat") void api.acpStop(sid);
    const next = vs.filter((_, j) => j !== i);
    setViews(sid, next, Math.max(0, Math.min(active >= i ? active - 1 : active, next.length - 1)));
  };

  const agentItems: (MenuItem | "sep")[] = [
    {
      label: "Перезапустить и продолжить",
      run: () => {
        const t = getTerm(`${sid}/agent`);
        void api
          .restartAgent(sid, t.term.cols, t.term.rows, false)
          .then(() => markRestarted(t.id))
          .catch((e) => toast(errText(e)));
      },
    },
    {
      label: "Новый разговор",
      run: () => {
        const t = getTerm(`${sid}/agent`);
        t.term.clear();
        void api
          .restartAgent(sid, t.term.cols, t.term.rows, true)
          .then(() => markRestarted(t.id))
          .catch((e) => toast(errText(e)));
      },
    },
    "sep",
    ...presets
      .filter((p) => p.id !== session.agent)
      .map(
        (p): MenuItem => ({
          label: `Сменить на ${p.name}`,
          run: async () => {
            await guard(api.setSessionAgent(sid, p.id));
            await reload();
            const t = getTerm(`${sid}/agent`);
            t.term.clear();
            await guard(api.restartAgent(sid, t.term.cols, t.term.rows, true));
            markRestarted(t.id);
          },
        }),
      ),
  ];

  return (
    <main className="pane center">
      <div className="sbar" role="tablist" aria-label="Сессии">
        {project.sessions.map((x) => (
          <button
            key={x.id}
            className="stab"
            role="tab"
            aria-selected={x.id === sid}
            title={x.path}
            onClick={() => void selectSession(project.id, x.id)}
            onContextMenu={(e) => {
              if (!x.worktree) return;
              e.preventDefault();
              setCloseFor({ session: x, x: e.clientX, y: e.clientY });
            }}
          >
            <span className={"dot " + (status[x.id] ?? "idle")} />
            {x.branch || "(нет ветки)"}
            {x.worktree && <span className="tag">worktree</span>}
            {x.worktree && (
              <span
                className="x"
                role="button"
                aria-label="Закрыть сессию"
                title="Закрыть сессию"
                onClick={(e) => {
                  e.stopPropagation();
                  const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
                  setCloseFor({ session: x, x: r.left, y: r.bottom + 6 });
                }}
              >
                ×
              </span>
            )}
          </button>
        ))}
        <button
          className="stab"
          title="Новая сессия в git worktree"
          aria-label="Новая сессия"
          onClick={(e) => {
            const r = e.currentTarget.getBoundingClientRect();
            setNewAt({ x: r.left, y: r.bottom + 6 });
          }}
        >
          +
        </button>
        <span className="grow" />
        <button
          className="agent-pill"
          onClick={(e) => {
            const r = e.currentTarget.getBoundingClientRect();
            setAgentMenu({ x: r.right - 220, y: r.bottom + 4 });
          }}
        >
          {preset?.name ?? session.agent} ▾
        </button>
      </div>

      <div className="vtabs" role="tablist">
        {views.map((v, i) => (
          <button
            key={v.type === "agent" || v.type === "chat" ? v.type : v.type === "shell" ? v.termId : v.path}
            className="vtab"
            role="tab"
            aria-selected={i === active}
            title={v.type === "file" ? v.path : v.type === "chat" ? "Чат через Agent Client Protocol" : undefined}
            onClick={() => setViews(sid, views, i)}
          >
            {v.type === "agent" ? "Агент" : v.type === "chat" ? "Чат" : v.type === "shell" ? (v.n > 1 ? `zsh ${v.n}` : "zsh") : v.path.split("/").pop()}
            {v.type !== "agent" && (
              <span
                className="x"
                role="button"
                aria-label="Закрыть"
                onClick={(e) => {
                  e.stopPropagation();
                  closeView(i);
                }}
              >
                ×
              </span>
            )}
          </button>
        ))}
        <button className="vtab" title="Новый shell в папке сессии" onClick={() => void addShell()}>
          + zsh
        </button>
        {!views.some((v) => v.type === "chat") && preset?.acp && (
          <button className="vtab" title={`Структурный чат через ACP: ${preset.acp}`} onClick={() => void openChat(sid)}>
            + Чат
          </button>
        )}
      </div>

      {views[active]?.type === "agent" && <AgentHints sessionId={sid} claude={!!preset?.claude} />}
      <div className="view">
        <TerminalView key={`${sid}/agent`} termId={`${sid}/agent`} sessionId={sid} kind="agent" visible={views[active]?.type === "agent"} />
        {views.map((v, i) =>
          v.type === "shell" ? (
            <TerminalView key={v.termId} termId={v.termId} sessionId={sid} kind="shell" visible={i === active} />
          ) : null,
        )}
        {views[active]?.type === "file" && <FilePreview key={(views[active] as { path: string }).path} sessionId={sid} path={(views[active] as { path: string }).path} />}
        {views[active]?.type === "chat" && <ChatView sessionId={sid} root={session.path} />}
      </div>

      {newAt && <NewSessionPopover project={project} at={newAt} onClose={() => setNewAt(null)} />}
      {closeFor && <CloseSessionPopover session={closeFor.session} at={closeFor} onClose={() => setCloseFor(null)} />}
      {agentMenu && <Menu x={agentMenu.x} y={agentMenu.y} items={agentItems} onClose={() => setAgentMenu(null)} />}
    </main>
  );
}
