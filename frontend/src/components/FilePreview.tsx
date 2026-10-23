import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowDown, ArrowUp, X } from "lucide-react";
import { api, errText } from "../lib/api";
import { guard } from "../lib/actions";
import { highlightFile } from "../lib/highlight";
import { buildRegex, defaultOpts, findRanges, markHtml, type SearchOpts } from "../lib/search";
import { setState, useUI } from "../lib/store";
import type { FileContent } from "../lib/types";
import { EditPopover, type EditTarget } from "./EditPopover";

/** Toggle buttons for match case / whole word / regex. */
export function SearchToggles(props: { opts: SearchOpts; onChange: (o: SearchOpts) => void; bad?: boolean }) {
  const t = (k: "case" | "word" | "regex", label: string, title: string) => (
    <button
      type="button"
      className={"stog" + (props.opts[k] ? " on" : "") + (k === "regex" && props.bad ? " bad" : "")}
      aria-pressed={props.opts[k]}
      title={title}
      onClick={() => props.onChange({ ...props.opts, [k]: !props.opts[k] })}
    >
      {label}
    </button>
  );
  return (
    <span className="stogs">
      {t("case", "Aa", "Учитывать регистр")}
      {t("word", "ab", "Слово целиком")}
      {t("regex", ".*", "Регулярное выражение")}
    </span>
  );
}

interface Match {
  line: number; // 1-based
  start: number;
  end: number;
}

export function FilePreview(props: { sessionId: string; path: string }) {
  const gitTick = useUI((s) => s.gitTick);
  const fsTick = useUI((s) => s.fsTick);
  const editor = useUI((s) => s.st?.data.settings.editor ?? "");
  const jump = useUI((s) => s.jump);
  const [file, setFile] = useState<FileContent | null>(null);
  const [err, setErr] = useState("");
  const [sel, setSel] = useState<{ a: number; b: number } | null>(null);
  const [edit, setEdit] = useState<EditTarget | null>(null);
  const [find, setFind] = useState<{ q: string; opts: SearchOpts } | null>(null);
  const [cur, setCur] = useState(0);
  const [flash, setFlash] = useState(0);
  const codeRef = useRef<HTMLDivElement>(null);
  const findRef = useRef<HTMLInputElement>(null);
  const pendingJump = useRef<number>(0);

  useEffect(() => {
    let alive = true;
    api
      .readFile(props.sessionId, props.path)
      .then((f) => alive && (setFile(f), setErr("")))
      .catch((e) => alive && setErr(errText(e)));
    return () => {
      alive = false;
    };
  }, [props.sessionId, props.path, gitTick, fsTick]);

  useEffect(() => setSel(null), [props.path]);

  const raw = useMemo(() => (file && !file.binary && !file.tooBig ? file.content.split("\n") : []), [file]);
  const lines = useMemo(() => (file && !file.binary && !file.tooBig ? highlightFile(file.content, file.path || props.path) : []), [file, props.path]);

  const re = useMemo(() => (find ? buildRegex(find.q, find.opts) : null), [find]);
  const bad = !!find?.q && !re;
  const matches = useMemo(() => {
    const out: Match[] = [];
    if (!re) return out;
    raw.forEach((t, i) => {
      for (const [a, b] of findRanges(t, re)) out.push({ line: i + 1, start: a, end: b });
    });
    return out;
  }, [raw, re]);
  const byLine = useMemo(() => {
    const m = new Map<number, { ranges: [number, number][]; first: number }>();
    matches.forEach((x, i) => {
      const e = m.get(x.line) ?? { ranges: [], first: i };
      e.ranges.push([x.start, x.end]);
      m.set(x.line, e);
    });
    return m;
  }, [matches]);
  const curIdx = matches.length ? Math.min(cur, matches.length - 1) : -1;

  const scrollTo = (line: number) => {
    codeRef.current?.querySelector(`[data-ln="${line}"]`)?.scrollIntoView({ block: "center" });
  };

  // Jumps from project search: scroll to the line and open find with the query.
  useEffect(() => {
    if (!jump || jump.sessionId !== props.sessionId || jump.path !== props.path || pendingJump.current === jump.seq) return;
    if (!file) return;
    pendingJump.current = jump.seq;
    setState(() => ({ jump: null })); // once: switching tabs back must not jump again
    setFlash(jump.line);
    if (jump.query) setFind({ q: jump.query, opts: jump.opts ?? defaultOpts });
    requestAnimationFrame(() => scrollTo(jump.line));
  }, [jump, file, props.sessionId, props.path]);

  // After a jump, make the first match on (or after) the jumped-to line current.
  useEffect(() => {
    if (!flash || !matches.length) return;
    const i = matches.findIndex((m) => m.line >= flash);
    setCur(i < 0 ? 0 : i);
  }, [flash, matches]);

  useEffect(() => {
    if (curIdx >= 0) scrollTo(matches[curIdx].line);
  }, [curIdx, matches]);

  // ⌘F opens find, ⌘G / ⇧⌘G step through matches.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!e.metaKey) return;
      if (e.code === "KeyF" && !e.shiftKey) {
        e.preventDefault();
        e.stopPropagation();
        const picked = String(window.getSelection() ?? "").trim();
        setFind((f) => ({ q: picked && !picked.includes("\n") ? picked : (f?.q ?? ""), opts: f?.opts ?? defaultOpts }));
        setTimeout(() => findRef.current?.select(), 0);
      } else if (e.code === "KeyG" && matches.length) {
        e.preventDefault();
        step(e.shiftKey ? -1 : 1);
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }); // eslint-disable-line react-hooks/exhaustive-deps

  const step = (d: number) => {
    if (!matches.length) return;
    setFlash(0);
    setCur((c) => (Math.min(c, matches.length - 1) + d + matches.length) % matches.length);
  };

  const lo = sel ? Math.min(sel.a, sel.b) : 0;
  const hi = sel ? Math.max(sel.a, sel.b) : 0;

  if (err) return <div className="empty">Не удалось открыть файл: {err}</div>;
  if (!file) return <div className="empty">Загрузка…</div>;

  return (
    <div className="view">
      {find && (
        <div className="findbar" role="search">
          <input
            ref={findRef}
            className={"find-in" + (bad ? " bad" : "")}
            autoFocus
            value={find.q}
            spellCheck={false}
            placeholder="Найти в файле"
            onChange={(e) => {
              setFlash(0);
              setCur(0);
              setFind({ ...find, q: e.target.value });
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                step(e.shiftKey ? -1 : 1);
              } else if (e.key === "Escape") {
                e.preventDefault();
                setFind(null);
                setFlash(0);
              }
            }}
          />
          <SearchToggles opts={find.opts} bad={bad} onChange={(opts) => setFind({ ...find, opts })} />
          <span className="find-count">{find.q ? (bad ? "ошибка в выражении" : matches.length ? `${curIdx + 1} из ${matches.length}` : "нет совпадений") : ""}</span>
          <button className="icon-btn sm" title="Предыдущее (⇧Enter)" aria-label="Предыдущее" disabled={!matches.length} onClick={() => step(-1)}>
            <ArrowUp size={14} />
          </button>
          <button className="icon-btn sm" title="Следующее (Enter)" aria-label="Следующее" disabled={!matches.length} onClick={() => step(1)}>
            <ArrowDown size={14} />
          </button>
          <button className="icon-btn sm" title="Закрыть (Esc)" aria-label="Закрыть поиск" onClick={() => (setFind(null), setFlash(0))}>
            <X size={14} />
          </button>
        </div>
      )}
      <div className="code" ref={codeRef}>
        {file.tooBig && <div className="empty">Файл больше 1 МБ — откройте его в редакторе.</div>}
        {file.binary && <div className="empty">Бинарный файл.</div>}
        {lines.map((t, i) => {
          const n = i + 1;
          const hit = byLine.get(n);
          const html = hit ? markHtml(t, hit.ranges, (k) => (hit.first + k === curIdx ? "fm cur" : "fm")) : t;
          return (
            <div key={n} data-ln={n} className={"cl" + (sel && n >= lo && n <= hi ? " sel" : "") + (n === flash ? " flash" : "")}>
              <span className="ln" onClick={(e) => setSel((s) => (e.shiftKey && s ? { a: s.a, b: n } : { a: n, b: n }))}>
                {n}
              </span>
              <span className="tx" dangerouslySetInnerHTML={{ __html: html || " " }} />
            </div>
          );
        })}
      </div>
      <div className="selbar">
        {sel ? (
          <>
            <span>
              {props.path.split("/").pop()}:{lo === hi ? lo : `${lo}–${hi}`}
            </span>
            <button
              className="btn sm primary"
              onClick={(e) => {
                const bar = (e.currentTarget.parentElement ?? e.currentTarget).getBoundingClientRect();
                setEdit({ sessionId: props.sessionId, path: props.path, from: lo, to: hi, x: bar.left + bar.width / 2, y: bar.top, place: "above-center" });
              }}
            >
              Edit…
            </button>
            <button className="btn sm ghost" onClick={() => setSel(null)}>
              Сбросить
            </button>
          </>
        ) : (
          <>
            <span>Клик по номеру строки — выделить · ⌘F — поиск</span>
            <button className="btn sm ghost" onClick={() => void guard(api.openExternal(props.sessionId, props.path, editor))}>
              {editor ? `Открыть в ${editor}` : "Открыть"}
            </button>
          </>
        )}
      </div>
      {edit && <EditPopover target={edit} onClose={() => setEdit(null)} />}
    </div>
  );
}
