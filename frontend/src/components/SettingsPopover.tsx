import { useState } from "react";
import { api } from "../lib/api";
import { guard, reload, saveSettings } from "../lib/actions";
import { activeProject, useUI, EMPTY } from "../lib/store";
import { Floating } from "./Floating";

export function SettingsPopover(props: { at: { x: number; y: number }; onClose: () => void }) {
  const settings = useUI((s) => s.st?.data.settings);
  const presets = useUI((s) => s.st?.presets ?? EMPTY);
  const project = useUI(activeProject);
  const [setup, setSetup] = useState(project?.setupScript ?? "");
  const [editor, setEditor] = useState(settings?.editor ?? "");
  const tmux = useUI((s) => s.st?.tmux ?? "");
  const glab = useUI((s) => s.st?.glab ?? "");
  const agentId = settings?.defaultAgent ?? "claude";
  const preset = presets.find((p) => p.id === agentId);
  const [acp, setAcp] = useState(settings?.acp?.[agentId] ?? "");
  if (!settings) return null;

  const close = async () => {
    if (project && setup !== project.setupScript) {
      await guard(api.updateProject(project.id, setup));
      await reload();
    }
    if (editor !== settings.editor) await saveSettings({ editor });
    if (acp.trim() !== (settings.acp?.[agentId] ?? "")) await saveSettings({ acp: { ...(settings.acp ?? {}), [agentId]: acp.trim() } });
    props.onClose();
  };

  return (
    <Floating x={props.at.x} y={props.at.y} onClose={() => void close()} className="pop" label="Настройки">
      <h3>Настройки</h3>
      <label>
        Агент для новых проектов и сессий
        <select className="field" value={settings.defaultAgent} onChange={(e) => void saveSettings({ defaultAgent: e.target.value })}>
          {presets.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </label>
      <label className="check">
        <input type="checkbox" checked={settings.autoSend} onChange={(e) => void saveSettings({ autoSend: e.target.checked })} />
        Edit и Submit сразу отправляют промпт
      </label>
      <label className="check">
        <input type="checkbox" checked={settings.showIgnored} onChange={(e) => void saveSettings({ showIgnored: e.target.checked })} />
        Показывать файлы из .gitignore
      </label>
      <label className="check" title={tmux ? `tmux: ${tmux}` : "brew install tmux"}>
        <input type="checkbox" checked={settings.tmux} disabled={!tmux} onChange={(e) => void saveSettings({ tmux: e.target.checked })} />
        Агенты в tmux — переживают перезапуск Ankh{tmux ? "" : " (tmux не найден: brew install tmux)"}
      </label>
      <label>
        Команда чата (ACP) · {preset?.name ?? agentId}
        <input className="field mono" value={acp} spellCheck={false} placeholder={preset?.acp || "команда, говорящая на ACP через stdio"} onChange={(e) => setAcp(e.target.value)} />
      </label>
      <label>
        Редактор для «Open in…»
        <input className="field" value={editor} placeholder="Cursor, Zed, Visual Studio Code" onChange={(e) => setEditor(e.target.value)} />
      </label>
      {project && (
        <label>
          Скрипт подготовки worktree · {project.name}
          <textarea
            className="field mono"
            value={setup}
            spellCheck={false}
            placeholder={'cp "$ANKH_PROJECT/.env" . && go mod download'}
            onChange={(e) => setSetup(e.target.value)}
          />
        </label>
      )}
      <div className="hintline">
        Агент запускается через login-shell: прокси и переменные из .zshrc подхватываются. $ANKH_PROJECT — папка основного проекта.
        {glab ? " GitLab MR — через ваш glab." : " Для MR установите glab и выполните glab auth login."}
      </div>
      <div className="acts">
        <span />
        <button className="btn sm primary" onClick={() => void close()}>
          Готово
        </button>
      </div>
    </Floating>
  );
}
