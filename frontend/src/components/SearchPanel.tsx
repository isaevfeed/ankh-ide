import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronRight } from "lucide-react";
import { api, errText } from "../lib/api";
import { openAt } from "../lib/actions";
import { defaultOpts, markText, type SearchOpts } from "../lib/search";
import { useUI } from "../lib/store";
import type { SearchHit, SearchResult } from "../lib/types";
import { FileIcon } from "../lib/fileicons";
import { SearchToggles } from "./FilePreview";

interface Saved {
  q: string;
  opts: SearchOpts;
  res: SearchResult | null;
  collapsed: Set<string>;
}
// Query and results per session survive switching tabs and panels.
const saved = new Map<string, Saved>();

const plural = (n: number, f: [string, string, string]) => {
  const a = n % 10, b = n % 100;
  return f[a === 1 && b !== 11 ? 0 : a >= 2 && a <= 4 && (b < 10 || b >= 20) ? 1 : 2];
};

/** Project-wide search (⌘⇧F): read-only, like the search panel of an IDE. */
export function SearchPanel(props: { sessionId: string }) {
  const sid = props.sessionId;
  const focusTick = useUI((s) => s.searchFocus);
  const fsTick = useUI((s) => s.fsTick);
  const init = saved.get(sid);
  const [q, setQ] = useState(init?.q ?? "");
  const [opts, setOpts] = useState<SearchOpts>(init?.opts ?? defaultOpts);
  const [showInclude, setShowInclude] = useState(!!init?.opts.include);
  const [res, setRes] = useState<SearchResult | null>(init?.res ?? null);
  const [collapsed, setCollapsed] = useState<Set<string>>(init?.collapsed ?? new Set());
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const seq = useRef(0);

  useEffect(() => {
    saved.set(sid, { q, opts, res, collapsed });
  }, [sid, q, opts, res, collapsed]);

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, [focusTick]);

  // Search as you type (debounced); again when files change on disk.
  useEffect(() => {
    if (!q.trim()) {
      setRes(null);
      setErr("");
      return;
    }
    const my = ++seq.current;
    const t = setTimeout(async () => {
      setBusy(true);
      try {
        const r = await api.search(sid, q, opts);
        if (my !== seq.current) return;
        setRes(r);
        setErr("");
      } catch (e) {
        if (my === seq.current) setErr(errText(e));
      } finally {
        if (my === seq.current) setBusy(false);
      }
    }, 250);
    return () => clearTimeout(t);
  }, [sid, q, opts, fsTick]);

  const groups = useMemo(() => {
    const m = new Map<string, SearchHit[]>();
    for (const h of res?.hits ?? []) {
      const g = m.get(h.path);
      if (g) g.push(h);
      else m.set(h.path, [h]);
    }
    return [...m.entries()];
  }, [res]);
  const total = res?.hits?.length ?? 0;

  const toggle = (p: string) =>
    setCollapsed((c) => {
      const n = new Set(c);
      if (n.has(p)) n.delete(p);
      else n.add(p);
      return n;
    });

  return (
    <div className="search">
      <div className="s-form">
        <div className="s-row">
          <input
            ref={inputRef}
            className="field s-in"
            value={q}
            autoFocus
            spellCheck={false}
            placeholder="Поиск по файлам"
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") setQ("");
            }}
          />
        </div>
        <div className="s-row">
          <SearchToggles opts={opts} bad={err.includes("регулярное")} onChange={setOpts} />
          <span className="grow" />
          <button className={"linkbtn" + (showInclude ? " on" : "")} onClick={() => setShowInclude(!showInclude)} title="Ограничить файлы">
            файлы…
          </button>
        </div>
        {showInclude && (
          <input
            className="field s-in"
            value={opts.include}
            spellCheck={false}
            placeholder="*.go, internal/, web/**/*.ts"
            onChange={(e) => setOpts({ ...opts, include: e.target.value })}
          />
        )}
        <div className="s-sum">
          {err ? (
            <span className="s-err">{err}</span>
          ) : !q.trim() ? (
            "Учитывает .gitignore. Клик по строке — открыть файл."
          ) : busy && !res ? (
            "Ищу…"
          ) : res ? (
            <>
              {total ? `${total}${res.truncated ? "+" : ""} ${plural(total, ["совпадение", "совпадения", "совпадений"])} в ${res.files} ${plural(res.files, ["файле", "файлах", "файлах"])}` : "Ничего не найдено"}
              {res.truncated && " · показаны первые"}
              {groups.length > 1 && (
                <button className="linkbtn" onClick={() => setCollapsed(collapsed.size ? new Set() : new Set(groups.map(([p]) => p)))}>
                  {collapsed.size ? "развернуть" : "свернуть"}
                </button>
              )}
            </>
          ) : null}
        </div>
      </div>
      <div className="s-res">
        {groups.map(([path, hits]) => {
          const dir = path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "";
          const name = path.slice(dir ? dir.length + 1 : 0);
          const closed = collapsed.has(path);
          return (
            <div key={path}>
              <button className="s-file" onClick={() => toggle(path)} title={path} aria-expanded={!closed}>
                <ChevronRight size={13} className={"tw" + (closed ? "" : " open")} />
                <FileIcon name={name} />
                <span className="s-name">{name}</span>
                <span className="s-dir">{dir}</span>
                <span className="s-n">{hits.length}</span>
              </button>
              {!closed &&
                hits.map((h) => (
                  <button key={h.line} className="s-hit" onClick={() => openAt(sid, h.path, h.line, q, opts)} title={`${h.path}:${h.line}`}>
                    <span className="s-ln">{h.line}</span>
                    <span className="s-tx" dangerouslySetInnerHTML={{ __html: markText(h.text.replace(/^\s+/, ""), shift(h)) }} />
                  </button>
                ))}
            </div>
          );
        })}
      </div>
    </div>
  );
}

/** Ranges shifted by the leading whitespace we trim for display. */
function shift(h: SearchHit): [number, number][] {
  const lead = h.text.length - h.text.replace(/^\s+/, "").length;
  return (h.ranges ?? []).map(([a, b]) => [Math.max(0, a - lead), Math.max(0, b - lead)] as [number, number]);
}
