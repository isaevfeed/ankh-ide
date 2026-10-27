import { useEffect, useState } from "react";
import { api, errText } from "../lib/api";
import { reload } from "../lib/actions";
import { toast, useUI, EMPTY } from "../lib/store";
import { disposeTerm } from "../lib/terminals";
import type { BranchList, Project, Session } from "../lib/types";
import { Floating } from "./Floating";

const slug = (s: string) => s.replace(/[^A-Za-z0-9._-]+/g, "-");

export function NewSessionPopover(props: { project: Project; at: { x: number; y: number }; onClose: () => void }) {
  const { project } = props;
  const presets = useUI((s) => s.st?.presets ?? EMPTY);
  const home = useUI((s) => s.st?.home ?? "~");
  const defaultAgent = useUI((s) => s.st?.data.settings.defaultAgent ?? "claude");
  const [branch, setBranch] = useState("feature/");
  const [bases, setBases] = useState<string[]>([]);
  const [base, setBase] = useState("");
  const [agent, setAgent] = useState(defaultAgent);
  const [setup, setSetup] = useState(project.setupScript.trim() !== "");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    void api.branches(project.id).then((bs) => {
      const list = bs ?? [];
      setBases(list);
      setBase(list[0] ?? "");
    });
  }, [project.id]);

  const path = `${home.replace(/^\/Users\/[^/]+/, "~")}/.ankh/worktrees/${slug(project.name)}/${slug(branch)}`;

  const go = async () => {
    const b = branch.trim();
    if (!b || b.endsWith("/") || b.endsWith("-")) {
      setErr("Укажите имя ветки целиком, например feature/search-filters");
      return;
    }
    setBusy(true);
    setErr("");
    try {
      await api.createSession(project.id, b, base, agent, setup);
      props.onClose();
      await reload();
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Floating x={props.at.x} y={props.at.y} onClose={props.onClose} className="pop" label="Новая сессия">
      <h3>Новая сессия · {project.name}</h3>
      <label>
        Ветка
        <input
          className="field mono"
          autoFocus
          spellCheck={false}
          value={branch}
          onChange={(e) => setBranch(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && void go()}
        />
      </label>
      <div className="two">
        <label>
          От ветки
          <select className="field" value={base} onChange={(e) => setBase(e.target.value)}>
            {bases.map((b) => (
              <option key={b}>{b}</option>
            ))}
          </select>
        </label>
        <label>
          Агент
          <select className="field" value={agent} onChange={(e) => setAgent(e.target.value)}>
            {presets.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
      </div>
      <label className="check">
        <input type="checkbox" checked={setup} disabled={!project.setupScript.trim()} onChange={(e) => setSetup(e.target.checked)} />
        {project.setupScript.trim() ? "Запустить скрипт подготовки" : "Скрипт подготовки не задан (настройки проекта)"}
      </label>
      <div className="hintline mono">{path}</div>
      {err && <div className="err">{err}</div>}
      <div className="acts">
        <span className="hintline">Создаст git worktree и запустит агента</span>
        <button className="btn sm primary" disabled={busy} onClick={() => void go()}>
          {busy ? "Создаю…" : "Создать"}
        </button>
      </div>
    </Floating>
  );
}

export function CloseSessionPopover(props: { session: Session; at: { x: number; y: number }; onClose: () => void }) {
  const { session } = props;
  const [delBranch, setDelBranch] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);

  const go = async (force: boolean) => {
    setBusy(true);
    try {
      await api.closeSession(session.id, force, delBranch);
      disposeTerm(`${session.id}/agent`);
      props.onClose();
      await reload();
      toast(`Сессия ${session.branch} закрыта`);
    } catch (e) {
      if (errText(e).includes("has_changes")) setDirty(true);
      else toast(errText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Floating x={props.at.x} y={props.at.y} onClose={props.onClose} className="pop" label="Закрыть сессию">
      <h3>Закрыть сессию {session.branch}?</h3>
      <div className="hintline mono">{session.path}</div>
      <p className="ptext">Агент остановится, папка worktree будет удалена. Ветка останется в репозитории, если не отметить ниже.</p>
      <label className="check">
        <input type="checkbox" checked={delBranch} onChange={(e) => setDelBranch(e.target.checked)} /> Удалить ветку {session.branch}
      </label>
      {dirty && <div className="err">В worktree есть незакоммиченные изменения. Они пропадут.</div>}
      <div className="acts">
        <button className="btn sm ghost" onClick={props.onClose}>
          Отмена
        </button>
        <button className="btn sm danger" disabled={busy} onClick={() => void go(dirty)}>
          {dirty ? "Удалить с изменениями" : "Закрыть сессию"}
        </button>
      </div>
    </Floating>
  );
}

type BranchRow = { name: string; remote: boolean } | { create: string };

/** Branches of the main session: filter, switch, or create one from HEAD. */
export function BranchPopover(props: { session: Session; at: { x: number; y: number }; onClose: () => void }) {
  const { session } = props;
  const [list, setList] = useState<BranchList | null>(null);
  const [q, setQ] = useState("");
  const [sel, setSel] = useState(0);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    api.listBranches(session.id).then(setList, (e) => setErr(errText(e)));
  }, [session.id]);

  const query = q.trim();
  const ql = query.toLowerCase();
  const local = (list?.local ?? EMPTY).filter((b) => b.toLowerCase().includes(ql));
  const remote = (list?.remote ?? EMPTY).filter((b) => b.toLowerCase().includes(ql));
  const exists = (list?.local ?? EMPTY).includes(query);
  const rows: BranchRow[] = [
    ...(query && !exists ? [{ create: query }] : []),
    ...local.map((name) => ({ name, remote: false })),
    ...remote.map((name) => ({ name, remote: true })),
  ];
  const cur = Math.min(sel, rows.length - 1);

  const go = async (r: BranchRow) => {
    if ("name" in r && r.name === list?.current) return props.onClose();
    setBusy(true);
    setErr("");
    try {
      const create = "create" in r;
      await api.switchBranch(session.id, create ? r.create : r.name, create);
      props.onClose();
      await reload();
      toast(create ? `Создана ветка ${r.create}` : `Переключено на ${r.remote ? r.name.slice(r.name.indexOf("/") + 1) : r.name}`);
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Floating x={props.at.x} y={props.at.y} onClose={props.onClose} className="pop branches" label="Ветки">
      <h3>Ветки · {list?.current || session.branch}</h3>
      {session.worktree ? (
        <p className="ptext">Ветка worktree-сессии закреплена. Для другой ветки создайте новую сессию (+) или переключитесь в основной сессии проекта.</p>
      ) : (
        <>
          <input
            className="field mono"
            autoFocus
            spellCheck={false}
            placeholder="Найти ветку или ввести имя новой"
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
              setSel(0);
            }}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                e.preventDefault();
                setSel(Math.max(0, Math.min(rows.length - 1, cur + (e.key === "ArrowDown" ? 1 : -1))));
              } else if (e.key === "Enter" && rows[cur] && !busy) {
                e.preventDefault();
                void go(rows[cur]);
              }
            }}
          />
          <div className="br-list" role="listbox" aria-label="Ветки">
            {!list && !err && <div className="hintline">Загружаю…</div>}
            {rows.map((r, i) => {
              const key = "create" in r ? "+" + r.create : r.name;
              const first = "name" in r && r.remote && (i === 0 || !("name" in rows[i - 1] && (rows[i - 1] as { remote: boolean }).remote));
              return (
                <div key={key}>
                  {first && <div className="br-sep">remote</div>}
                  <button
                    className={"br-row" + ("name" in r && r.name === list?.current ? " current" : "")}
                    role="option"
                    aria-selected={i === cur}
                    disabled={busy}
                    onMouseEnter={() => setSel(i)}
                    onClick={() => void go(r)}
                  >
                    {"create" in r ? (
                      <>
                        <span className="br-plus">+</span> Создать ветку <b className="mono">{r.create}</b> от {list?.current}
                      </>
                    ) : (
                      <>
                        <span className="br-check">{r.name === list?.current ? "✓" : ""}</span>
                        <span className="mono">{r.name}</span>
                      </>
                    )}
                  </button>
                </div>
              );
            })}
            {list && rows.length === 0 && <div className="hintline">Ничего не найдено</div>}
          </div>
          <div className="hintline">↑↓ — выбор · Enter — переключиться. Незакоммиченные правки переедут с вами, если не конфликтуют.</div>
        </>
      )}
      {err && <div className="err">{err}</div>}
    </Floating>
  );
}
