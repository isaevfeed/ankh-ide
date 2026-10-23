import { useEffect } from "react";
import { addProject, reload, selectProject, wireEvents } from "./lib/actions";
import { activeProject, activeSession, getState, setState, useUI } from "./lib/store";
import { TitleBar, openSettingsShortcut } from "./components/TitleBar";
import { FileTree } from "./components/FileTree";
import { Center } from "./components/Center";
import { Changes } from "./components/Changes";
import { UsageBar } from "./components/UsageBar";
import { Splitter } from "./components/Splitter";
import { QuickOpen } from "./components/QuickOpen";
import { SearchPanel } from "./components/SearchPanel";

function Toasts() {
  const toasts = useUI((s) => s.toasts);
  return (
    <div className="toasts" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className="toast">
          <span>{t.text}</span>
          {t.action && (
            <button
              onClick={() => {
                t.action!.run();
                setState((s) => ({ toasts: s.toasts.filter((x) => x.id !== t.id) }));
              }}
            >
              {t.action.label}
            </button>
          )}
        </div>
      ))}
    </div>
  );
}

function Welcome() {
  return (
    <div className="welcome">
      <div className="ankh" aria-hidden="true">☥</div>
      <h1>Ankh</h1>
      <p>Откройте папку проекта. Агент запустится в терминале, изменения появятся справа — их можно отревьюить построчно и отправить замечания одним промптом.</p>
      <button className="btn primary" onClick={() => void addProject()}>
        Открыть проект…
      </button>
    </div>
  );
}

export function App() {
  const loaded = useUI((s) => s.st !== null);
  const project = useUI(activeProject);
  const session = useUI(activeSession);
  const showFiles = useUI((s) => s.showFiles);
  const showChanges = useUI((s) => s.showChanges);
  const widths = useUI((s) => s.widths);
  const quickOpen = useUI((s) => s.quickOpen);
  const sidebar = useUI((s) => s.sidebar);

  useEffect(() => {
    wireEvents();
    void reload();
    const onKey = (e: KeyboardEvent) => {
      if (!e.metaKey) return;
      if (e.code === "KeyF" && e.shiftKey) {
        e.preventDefault();
        e.stopPropagation();
        setState((s) => ({ showFiles: true, sidebar: "search", searchFocus: s.searchFocus + 1 }));
      } else if (e.code === "KeyP" && !e.shiftKey && !e.altKey) {
        // capture phase: xterm must not see it
        e.preventDefault();
        e.stopPropagation();
        if (activeSession(getState())) setState((s) => ({ quickOpen: !s.quickOpen }));
      } else if (e.code === "KeyB") {
        e.preventDefault();
        if (e.altKey) setState((s) => ({ showChanges: !s.showChanges }));
        else setState((s) => ({ showFiles: !s.showFiles }));
      } else if (/^Digit[1-9]$/.test(e.code)) {
        const ps = getState().st?.data.projects ?? [];
        const p = ps[Number(e.code.slice(5)) - 1];
        if (p) {
          e.preventDefault();
          void selectProject(p.id);
        }
      } else if (e.key === ",") {
        e.preventDefault();
        openSettingsShortcut();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, []);

  if (!loaded) return <div className="app" />;

  const cols = `${showFiles ? widths.files + "px" : "0px"} minmax(0,1fr) ${showChanges ? widths.changes + "px" : "0px"}`;

  return (
    <div className="app">
      <TitleBar />
      {project && session ? (
        <div className="body" style={{ gridTemplateColumns: cols }}>
          {showFiles ? (
            <aside className="pane files" aria-label="Файлы">
              <div className="pane-head side-tabs" role="tablist">
                <button className="label stab-l" role="tab" aria-selected={sidebar === "files"} onClick={() => setState(() => ({ sidebar: "files" }))}>
                  Файлы
                </button>
                <button
                  className="label stab-l"
                  role="tab"
                  aria-selected={sidebar === "search"}
                  title="Поиск по файлам (⇧⌘F)"
                  onClick={() => setState((s) => ({ sidebar: "search", searchFocus: s.searchFocus + 1 }))}
                >
                  Поиск
                </button>
                <span className="grow" />
                <span className="sub">{session.worktree ? "worktree" : project.name}</span>
              </div>
              {sidebar === "files" ? <FileTree key={session.id} session={session} /> : <SearchPanel key={session.id} sessionId={session.id} />}
              <Splitter pane="files" other={showChanges ? widths.changes : 0} />
            </aside>
          ) : (
            <span />
          )}
          <Center project={project} session={session} />
          {showChanges ? <Changes key={session.id} session={session} splitter={<Splitter pane="changes" other={showFiles ? widths.files : 0} />} /> : <span />}
        </div>
      ) : (
        <Welcome />
      )}
      {quickOpen && session && <QuickOpen key={session.id} sessionId={session.id} />}
      <UsageBar />
      <Toasts />
    </div>
  );
}
