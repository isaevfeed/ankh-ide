import { useState } from "react";
import { api } from "../lib/api";
import { addProject, guard, reload, selectProject } from "../lib/actions";
import { activeProject, projectStatus, setState, useUI, EMPTY } from "../lib/store";
import { Menu } from "./Floating";
import { SettingsPopover } from "./SettingsPopover";

export function TitleBar() {
  const projects = useUI((s) => s.st?.data.projects ?? EMPTY);
  const current = useUI(activeProject);
  const ui = useUI((s) => s);
  const showFiles = useUI((s) => s.showFiles);
  const showChanges = useUI((s) => s.showChanges);
  const [menu, setMenu] = useState<{ id: string; name: string; x: number; y: number } | null>(null);
  const [settingsAt, setSettingsAt] = useState<{ x: number; y: number } | null>(null);

  return (
    <div className="titlebar">
      <div className="ptabs" role="tablist" aria-label="Проекты">
        {projects.map((p, i) => (
          <button
            key={p.id}
            className="ptab"
            role="tab"
            aria-selected={p.id === current?.id}
            title={`${p.path}${i < 9 ? `  (⌘${i + 1})` : ""}`}
            onClick={() => void selectProject(p.id)}
            onContextMenu={(e) => {
              e.preventDefault();
              setMenu({ id: p.id, name: p.name, x: e.clientX, y: e.clientY });
            }}
          >
            <span className={"dot " + projectStatus(ui, p)} />
            <span className="name">{p.name}</span>
          </button>
        ))}
        <button className="ptab add" title="Открыть папку как проект" aria-label="Добавить проект" onClick={() => void addProject()}>
          +
        </button>
      </div>
      <div className="tools">
        <button
          className="icon-btn"
          aria-pressed={showFiles}
          title="Файлы (⌘B)"
          aria-label="Показать или скрыть файлы"
          onClick={() => setState((s) => ({ showFiles: !s.showFiles }))}
        >
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.3">
            <rect x="2" y="3" width="12" height="10" rx="2" />
            <path d="M6 3v10" />
          </svg>
        </button>
        <button
          className="icon-btn"
          aria-pressed={showChanges}
          title="Изменения (⌘⌥B)"
          aria-label="Показать или скрыть изменения"
          onClick={() => setState((s) => ({ showChanges: !s.showChanges }))}
        >
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.3">
            <rect x="2" y="3" width="12" height="10" rx="2" />
            <path d="M10 3v10" />
          </svg>
        </button>
        <button
          className="icon-btn"
          title="Настройки (⌘,)"
          aria-label="Настройки"
          onClick={(e) => {
            const r = e.currentTarget.getBoundingClientRect();
            setSettingsAt({ x: r.right - 380, y: r.bottom + 6 });
          }}
        >
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round">
            <path d="M2.5 5h6M11.5 5h2M2.5 11h2M7.5 11h6" />
            <circle cx="10" cy="5" r="1.5" />
            <circle cx="6" cy="11" r="1.5" />
          </svg>
        </button>
      </div>
      {menu && (
        <Menu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          items={[
            {
              label: `Убрать «${menu.name}» из Ankh`,
              hint: "файлы не удаляются",
              danger: true,
              run: async () => {
                await guard(api.removeProject(menu.id));
                await reload();
              },
            },
          ]}
        />
      )}
      {settingsAt && <SettingsPopover at={settingsAt} onClose={() => setSettingsAt(null)} />}
    </div>
  );
}

export function openSettingsShortcut(): void {
  document.querySelector<HTMLButtonElement>('.tools .icon-btn[aria-label="Настройки"]')?.click();
}
