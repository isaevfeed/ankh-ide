import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { api, errText } from "../lib/api";
import { deliver, fileRef, openFile, recentFiles, rememberOpened, showAgent } from "../lib/actions";
import { fuzzyFilter, type Match } from "../lib/fuzzy";
import { setState, toast, useUI } from "../lib/store";
import { FileIcon } from "../lib/fileicons";

// File lists per session, refreshed when files change on disk.
const listCache = new Map<string, { tick: number; files: string[] }>();

function Highlighted(props: { m: Match }) {
  const { path, hits } = props.m;
  const set = new Set(hits);
  const base = path.lastIndexOf("/") + 1;
  const part = (from: number, to: number) => {
    const out = [];
    for (let i = from; i < to; i++) out.push(set.has(i) ? <b key={i}>{path[i]}</b> : path[i]);
    return out;
  };
  return (
    <>
      <span className="qn">{part(base, path.length)}</span>
      {base > 0 && <span className="qd">{part(0, base - 1)}</span>}
    </>
  );
}

/** ⌘P: find a file by name. Enter — preview, ⌘Enter — Edit…, ⇧Enter — insert @path into the agent's input. */
export function QuickOpen(props: { sessionId: string }) {
  const sid = props.sessionId;
  const fsTick = useUI((s) => s.fsTick);
  const [files, setFiles] = useState<string[] | null>(() => listCache.get(sid)?.files ?? null);
  const [q, setQ] = useState("");
  const [sel, setSel] = useState(0);
  const [prompt, setPrompt] = useState<string | null>(null);
  const [promptText, setPromptText] = useState("");
  const listRef = useRef<HTMLDivElement>(null);
  const close = () => setState(() => ({ quickOpen: false }));

  useEffect(() => {
    const c = listCache.get(sid);
    if (c && c.tick === fsTick) return;
    let alive = true;
    api
      .listFiles(sid)
      .then((f) => {
        if (!alive) return;
        listCache.set(sid, { tick: fsTick, files: f ?? [] });
        setFiles(f ?? []);
      })
      .catch((e) => alive && toast(errText(e)));
    return () => {
      alive = false;
    };
  }, [sid, fsTick]);

  const matches = useMemo(() => fuzzyFilter(q, files ?? [], 60, recentFiles(sid)), [q, files, sid]);
  useEffect(() => setSel(0), [q]);
  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>(`[data-i="${sel}"]`)?.scrollIntoView({ block: "nearest" });
  }, [sel]);

  const open = (path: string) => {
    openFile(sid, path);
    close();
  };

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault();
      close();
    } else if (e.key === "ArrowDown" || (e.ctrlKey && e.key === "n")) {
      e.preventDefault();
      setSel((i) => Math.min(i + 1, matches.length - 1));
    } else if (e.key === "ArrowUp" || (e.ctrlKey && e.key === "p")) {
      e.preventDefault();
      setSel((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const m = matches[sel];
      if (!m) return;
      if (e.metaKey) {
        setPrompt(m.path);
      } else if (e.shiftKey) {
        rememberOpened(sid, m.path);
        close();
        showAgent(sid);
        void api.sendPrompt(sid, fileRef(sid, m.path) + " ", false).catch((err) => toast(errText(err)));
      } else open(m.path);
    }
  };

  return createPortal(
    <div className="qo-back" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div className="qo" role="dialog" aria-label="Быстрый поиск файла">
        {prompt ? (
          <>
            <div className="qo-title mono">Edit · {fileRef(sid, prompt).replace(/^@/, "")}</div>
            <textarea
              className="field"
              autoFocus
              value={promptText}
              placeholder="Что изменить в этом файле?"
              onChange={(e) => setPromptText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  e.preventDefault();
                  setPrompt(null);
                } else if (e.key === "Enter" && !e.shiftKey && promptText.trim()) {
                  e.preventDefault();
                  rememberOpened(sid, prompt);
                  close();
                  void deliver(sid, `${fileRef(sid, prompt)} — ${promptText.trim()}`);
                }
              }}
            />
            <div className="qo-foot">Enter — отправить агенту · Esc — назад</div>
          </>
        ) : (
          <>
            <input
              className="qo-input"
              autoFocus
              value={q}
              spellCheck={false}
              placeholder={files ? `Найти файл · ${files.length.toLocaleString("ru")} в проекте` : "Индексирую файлы…"}
              onChange={(e) => setQ(e.target.value)}
              onKeyDown={onKey}
            />
            <div className="qo-list" ref={listRef} role="listbox">
              {files && matches.length === 0 && <div className="qo-empty">Ничего не найдено</div>}
              {matches.map((m, i) => (
                <div
                  key={m.path}
                  data-i={i}
                  role="option"
                  aria-selected={i === sel}
                  className="qo-row"
                  onMouseMove={() => setSel(i)}
                  onClick={() => open(m.path)}
                >
                  <FileIcon name={m.path.slice(m.path.lastIndexOf("/") + 1)} />
                  <Highlighted m={m} />
                </div>
              ))}
            </div>
            <div className="qo-foot">↑↓ — выбор · Enter — открыть · ⌘Enter — Edit… · ⇧Enter — вставить @путь агенту</div>
          </>
        )}
      </div>
    </div>,
    document.body,
  );
}
