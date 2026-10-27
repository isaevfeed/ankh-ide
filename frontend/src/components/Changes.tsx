import { useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject, type ReactNode } from "react";
import { api, errText } from "../lib/api";
import { chatActive, guard, reload, saveSettings, showAgent, suggestClear } from "../lib/actions";
import { highlight } from "../lib/highlight";
import { anchoredHere, commentKey, draftComment, firstCol, locString, newLineOf, rowKey } from "../lib/review";
import { toast, useUI, EMPTY } from "../lib/store";
import type { Comment, DiffMode, DiffResult, FileDiff, MRInfo, Session } from "../lib/types";
import { Check, Minus, Undo2 } from "lucide-react";
import { MRBar } from "./MRBar";

interface Form {
  fi: number;
  hi: number;
  li: number;
  liE: number | null;
  col: number | null;
  text: string; // initial text; what is typed lives in CommentForm
  editId?: string;
}

const plural = (n: number, f: [string, string, string]) => {
  const a = n % 10, b = n % 100;
  return f[a === 1 && b !== 11 ? 0 : a >= 2 && a <= 4 && (b < 10 || b >= 20) ? 1 : 2];
};

function colFromPoint(codeEl: HTMLElement, x: number, y: number): number | null {
  const d = document as Document & { caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null };
  let node: Node | null = null;
  let off = 0;
  if (d.caretPositionFromPoint) {
    const p = d.caretPositionFromPoint(x, y);
    if (p) [node, off] = [p.offsetNode, p.offset];
  } else if (document.caretRangeFromPoint) {
    const r = document.caretRangeFromPoint(x, y);
    if (r) [node, off] = [r.startContainer, r.startOffset];
  }
  if (!node || !codeEl.contains(node)) return null;
  const r = document.createRange();
  r.setStart(codeEl, 0);
  r.setEnd(node, off);
  return r.toString().length + 1;
}

// Last diff per session and mode: shown at once, then refreshed.
const diffCache = new Map<string, DiffResult>();
// Uncommitted file count per session, to notice when work gets committed.
const wipCount = new Map<string, number>();
// Unfinished commit messages per session.
const commitMsgs = new Map<string, string>();

// The comment and commit fields keep their text in their own state: a keystroke
// must not re-render a diff of thousands of rows.

function CommentForm(props: {
  initial: string;
  draft: MutableRefObject<string>; // last typed text, carried to a re-opened form
  focusKey: string;
  editing: boolean;
  rangeHint: boolean;
  onSave: (text: string) => void;
  onCancel: () => void;
}) {
  const [text, setText] = useState(props.initial);
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    props.draft.current = props.initial;
  }, []);
  useEffect(() => {
    ref.current?.focus();
  }, [props.focusKey]);
  return (
    <>
      <textarea
        ref={ref}
        className="field"
        placeholder="Что поправить? Например: переименуй переменную"
        value={text}
        onChange={(e) => {
          props.draft.current = e.target.value;
          setText(e.target.value);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            props.onSave(text);
          }
          if (e.key === "Escape") props.onCancel();
        }}
      />
      <div className="cactions">
        <span className="note">⌘Enter — сохранить · Esc — отмена{props.rangeHint ? " · Shift+клик по + — диапазон" : ""}</span>
        <button className="btn sm ghost" onClick={props.onCancel}>
          Отмена
        </button>
        <button className="btn sm primary" onClick={() => props.onSave(text)} disabled={!text.trim()}>
          {props.editing ? "Сохранить" : "Добавить"}
        </button>
      </div>
    </>
  );
}

function CommitBox(props: { sid: string; stagedN: number; onCommitted: () => Promise<void> }) {
  const [msg, setMsgRaw] = useState(() => commitMsgs.get(props.sid) ?? "");
  const [committing, setCommitting] = useState(false);
  const setMsg = (v: string) => {
    commitMsgs.set(props.sid, v);
    setMsgRaw(v);
  };

  const commit = async () => {
    if (!msg.trim() || committing) return;
    setCommitting(true);
    try {
      const h = await api.commit(props.sid, msg, props.stagedN === 0);
      setMsg("");
      toast(`Закоммичено: ${h}`);
      await props.onCommitted();
    } catch (e) {
      toast(errText(e));
    } finally {
      setCommitting(false);
    }
  };

  return (
    <div className="commit">
      <textarea
        className="field mono"
        rows={1}
        value={msg}
        spellCheck={false}
        placeholder={props.stagedN ? `Сообщение коммита · в индексе: ${props.stagedN}` : "Сообщение коммита"}
        onChange={(e) => setMsg(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            void commit();
          }
        }}
      />
      <button className="btn" disabled={!msg.trim() || committing} onClick={() => void commit()} title="⌘Enter">
        {committing ? "Коммичу…" : props.stagedN ? "Commit" : "Commit all"}
      </button>
    </div>
  );
}

function PushBar(props: { sid: string; ahead: number; upstream: string; onPushed: () => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const push = async () => {
    setBusy(true);
    try {
      await api.push(props.sid);
      toast(props.upstream ? `Запушено в ${props.upstream}` : "Ветка опубликована");
      await props.onPushed();
    } catch (e) {
      toast(errText(e));
    } finally {
      setBusy(false);
    }
  };
  const n = `${props.ahead} ${plural(props.ahead, ["коммит", "коммита", "коммитов"])}`;
  return (
    <div className="pushbar">
      <span className="sub">{props.upstream ? `↑ ${n} → ${props.upstream}` : `Ветка не опубликована · ${n}`}</span>
      <button className="btn sm" disabled={busy} onClick={() => void push()}>
        {busy ? "Пушу…" : props.upstream ? "Push" : "Опубликовать"}
      </button>
    </div>
  );
}

export function Changes(props: { session: Session; splitter?: ReactNode }) {
  const sid = props.session.id;
  const comments: Comment[] = props.session.comments ?? [];
  const mode = useUI((s) => s.st?.data.settings.diffMode ?? "wip");
  const autoSend = useUI((s) => s.st?.data.settings.autoSend ?? true);
  const gitTick = useUI((s) => s.gitTick);
  const [diff, setDiff] = useState<DiffResult | null>(() => diffCache.get(`${props.session.id}:${mode}`) ?? null);
  const [err, setErr] = useState("");
  const [form, setForm] = useState<Form | null>(null);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const [busy, setBusy] = useState(false);
  const [discardAsk, setDiscardAsk] = useState<string[] | null>(null);
  const [mr, setMR] = useState<MRInfo | null>(null);
  const [confirmed, setConfirmed] = useState<string[]>([]);
  const scroller = useRef<HTMLDivElement>(null);
  const draft = useRef("");

  const load = useCallback(async () => {
    try {
      const d = await api.getDiff(sid, mode);
      diffCache.set(`${sid}:${mode}`, d);
      if (mode === "wip") {
        const n = (d.files ?? []).length;
        const prev = wipCount.get(sid);
        if (prev !== undefined && prev > 0 && n === 0) suggestClear(sid, "изменения закоммичены — задача закрыта");
        wipCount.set(sid, n);
      }
      setDiff(d);
      setErr("");
      if (d.relocated) await reload(); // comments followed their lines
    } catch (e) {
      setErr(errText(e));
    }
  }, [sid, mode]);

  useEffect(() => {
    setDiff(diffCache.get(`${sid}:${mode}`) ?? null);
    setForm(null);
    setConfirmCancel(false);
    setDiscardAsk(null);
    void load();
  }, [sid, mode, load]);

  useEffect(() => {
    if (gitTick > 0) void load();
  }, [gitTick, load]);

  const files: FileDiff[] = diff?.files ?? [];
  const drafts = comments.filter((c) => !c.sent);
  const sentCount = comments.length - drafts.length;

  // Highlighting and comment lookup are done once per diff, not per render.
  const html = useMemo(() => files.map((f) => (f.hunks ?? EMPTY).map((h) => h.lines.map((l) => highlight(l.text, f.path) || " "))), [files]);
  const byRow = useMemo(() => {
    const m = new Map<string, Comment[]>();
    for (const c of comments) {
      const k = commentKey(c);
      const list = m.get(k);
      if (list) list.push(c);
      else m.set(k, [c]);
    }
    return m;
  }, [comments]);

  // Comments that do not land on any row of the current diff.
  const orphans = useMemo(() => {
    return comments.filter((c) => {
      const f = files.find((x) => x.path === c.path);
      return !f || !(f.hunks ?? EMPTY).some((h) => h.lines.some((l) => anchoredHere(c, f.path, l)));
    });
  }, [comments, files]);

  const openForm = (fi: number, hi: number, li: number, col: number | null, shift: boolean) => {
    if (shift && form && !form.editId && form.fi === fi && form.hi === hi && li !== form.li) {
      setForm({ ...form, liE: li, col: null, text: draft.current });
      return;
    }
    setConfirmCancel(false);
    setForm({ fi, hi, li, liE: null, col, text: form && !form.editId ? draft.current : "" });
  };

  const save = async (typed: string) => {
    if (!form) return;
    const text = typed.trim();
    if (!text) return;
    try {
      if (form.editId) {
        await api.updateComment(sid, form.editId, text);
      } else {
        const f = files[form.fi];
        const h = (f.hunks ?? EMPTY)[form.hi];
        await api.addComment(sid, draftComment(f.path, h, form.li, form.liE, form.col, text));
      }
      setForm(null);
      await reload();
    } catch (e) {
      toast(errText(e));
    }
  };

  const submit = async () => {
    setBusy(true);
    const chat = chatActive(sid);
    if (!chat) showAgent(sid);
    try {
      await api.submitReview(sid, chat);
      toast(autoSend || chat ? `Отправлено агенту: ${drafts.length} ${plural(drafts.length, ["замечание", "замечания", "замечаний"])}` : "Замечания вставлены в поле ввода агента");
      await reload();
    } catch (e) {
      toast(errText(e));
    } finally {
      setBusy(false);
    }
  };

  const gitOp = async (p: Promise<unknown>) => {
    const ok = await guard(p.then(() => true));
    if (ok) await load();
    return !!ok;
  };
  const pathsOf = (f: FileDiff) => (f.oldPath && f.oldPath !== f.path ? [f.path, f.oldPath] : [f.path]);
  const index = diff?.mode === "wip" ? (diff.index ?? {}) : null;
  const stagedN = index ? Object.values(index).filter((v) => v).length : 0;

  const toMR = async () => {
    setBusy(true);
    try {
      toast(await api.mrPostComments(sid));
      await reload();
    } catch (e) {
      toast(errText(e));
    } finally {
      setBusy(false);
    }
  };

  // A sent comment the user checked: shown green for a moment, then removed.
  const confirm = (id: string) => {
    setConfirmed((xs) => [...xs, id]);
    setTimeout(() => {
      void api
        .deleteComment(sid, id)
        .then(reload)
        .catch((e) => toast(errText(e)))
        .finally(() => setConfirmed((xs) => xs.filter((x) => x !== id)));
    }, 900);
  };

  const afterCommit = async () => {
    await load();
    await reload(); // sent comments are gone with the commit
  };

  const discard = async (all: boolean) => {
    await api.discardDrafts(sid, all).catch((e) => toast(errText(e)));
    setConfirmCancel(false);
    setForm(null);
    await reload();
  };

  const commentBox = (c: Comment, outdated: boolean) => (
    <div key={c.id} className={"cbox" + (c.sent ? " sent" : "") + (confirmed.includes(c.id) ? " confirmed" : "")}>
      <div className="cmeta">
        <b>{c.loc.slice(c.path.length + 1)}{c.deleted ? " · удалённый код" : ""}</b>
        {c.author && <span className="badge" title="Замечание из GitLab MR">@{c.author}</span>}
        {outdated && <span className="badge warn" title="Строки с этим кодом больше нет в файле">строка изменилась</span>}
        {!outdated && c.movedFrom ? <span className="badge" title="Код сдвинулся, замечание переехало вместе с ним">со строки {c.movedFrom}</span> : null}
        {confirmed.includes(c.id) ? (
          <span className="badge ok">исправлено</span>
        ) : c.sent ? (
          <>
            <span className="badge">{c.target === "mr" ? "в MR" : "отправлено"}</span>
            <span className="cactions">
              <button className="btn sm ok" title="Правка верна — убрать замечание" onClick={() => confirm(c.id)}>
                Верно ✓
              </button>
            </span>
          </>
        ) : (
          <span className="cactions">
            <button className="btn sm ghost" onClick={() => setForm({ fi: -1, hi: -1, li: -1, liE: null, col: null, text: c.text, editId: c.id })}>
              Изменить
            </button>
            <button className="btn sm ghost" onClick={() => void api.deleteComment(sid, c.id).then(reload)}>
              Удалить
            </button>
          </span>
        )}
      </div>
      {form?.editId === c.id ? formBox(c.loc) : <div className="ctext">{c.text}</div>}
    </div>
  );

  const formBox = (loc: string, deleted?: boolean) => (
    <div className="cform">
      {!form?.editId && (
        <div className="cmeta">
          <b>{loc}</b>
          {deleted && <span className="badge">удалённый код</span>}
        </div>
      )}
      {form && (
        <CommentForm
          initial={form.text}
          draft={draft}
          focusKey={`${form.fi}:${form.hi}:${form.li}:${form.liE}:${form.editId}`}
          editing={!!form.editId}
          rangeHint={form.liE === null && !form.editId}
          onSave={(t) => void save(t)}
          onCancel={() => setForm(null)}
        />
      )}
    </div>
  );

  const totA = files.reduce((a, f) => a + f.add, 0);
  const totD = files.reduce((a, f) => a + f.del, 0);

  return (
    <aside className="pane changes" aria-label="Изменения">
      {props.splitter}
      <div className="pane-head">
        <span className="label">Изменения</span>
        <select className="mode" value={mode} aria-label="С чем сравнивать" onChange={(e) => void saveSettings({ diffMode: e.target.value as DiffMode })}>
          <option value="wip">Незакоммиченное</option>
          <option value="branch">Вся ветка vs {diff?.mode === "branch" && diff.base ? diff.base : "develop"}</option>
          <option value="last">Последний коммит</option>
        </select>
      </div>

      <MRBar sessionId={sid} onInfo={setMR} />

      {err && <div className="empty">{err}</div>}
      {!err && diff && files.length === 0 && (
        <div className="empty">
          {mode === "wip" ? "Нет незакоммиченных изменений." : "Изменений нет."}
          <br />
          Когда агент что-то поменяет, файлы появятся здесь.
        </div>
      )}

      {files.length > 0 && (
        <>
          <div className="flist">
            {files.map((f, fi) => {
              const dir = f.path.includes("/") ? f.path.slice(0, f.path.lastIndexOf("/") + 1) : "";
              const n = drafts.filter((c) => c.path === f.path).length;
              const ix = index ? (index[f.path] ?? "") : null;
              const asking = discardAsk && discardAsk[0] === f.path && discardAsk.length <= 2;
              if (asking)
                return (
                  <div key={f.path} className="frow ask">
                    <span className="p">
                      Откатить <b>{f.path.slice(dir.length)}</b>?{f.status === "A" ? " Новый файл будет удалён." : ""}
                    </span>
                    <button className="btn sm ghost" onClick={() => setDiscardAsk(null)}>
                      Нет
                    </button>
                    <button
                      className="btn sm danger"
                      onClick={() => {
                        setDiscardAsk(null);
                        void gitOp(api.discard(sid, pathsOf(f)));
                      }}
                    >
                      Откатить
                    </button>
                  </div>
                );
              return (
                <div
                  key={f.path}
                  role="button"
                  tabIndex={0}
                  className="frow"
                  title={f.oldPath ? `${f.oldPath} → ${f.path}` : f.path}
                  onClick={() => {
                    const el = document.getElementById(`df-${fi}`);
                    if (el && scroller.current) scroller.current.scrollTop = el.offsetTop - scroller.current.offsetTop;
                  }}
                >
                  {ix !== null && (
                    <button
                      className={"stage " + (ix || "none")}
                      title={ix === "staged" ? "В индексе — убрать (unstage)" : ix === "partial" ? "Частично в индексе — добавить всё (stage)" : "Добавить в индекс (stage)"}
                      aria-label={ix === "staged" ? "Unstage" : "Stage"}
                      onClick={(e) => {
                        e.stopPropagation();
                        void gitOp(ix === "staged" ? api.unstage(sid, pathsOf(f)) : api.stage(sid, pathsOf(f)));
                      }}
                    >
                      {ix === "staged" ? <Check size={11} strokeWidth={3} /> : ix === "partial" ? <Minus size={11} strokeWidth={3} /> : null}
                    </button>
                  )}
                  <span className={"st " + f.status}>{f.status}</span>
                  <span className="p">
                    <i>{dir}</i>
                    {f.path.slice(dir.length)}
                  </span>
                  {n > 0 && <span className="cbadge">{n}</span>}
                  <span className="nums">
                    <span className="a">+{f.add}</span> <span className="d">−{f.del}</span>
                  </span>
                  {ix !== null && (
                    <button
                      className="discard"
                      title="Откатить изменения файла"
                      aria-label="Откатить изменения файла"
                      onClick={(e) => {
                        e.stopPropagation();
                        setDiscardAsk(pathsOf(f));
                      }}
                    >
                      <Undo2 size={13} />
                    </button>
                  )}
                </div>
              );
            })}
          </div>
          <div className="pane-head sub-head">
            <span className="sub">
              {files.length} {plural(files.length, ["файл", "файла", "файлов"])} ·{" "}
              <span className="nums">
                <span className="a">+{totA}</span> <span className="d">−{totD}</span>
              </span>
            </span>
            {index ? (
              discardAsk && discardAsk.length > 2 ? (
                <span className="ask-all">
                  Откатить все {files.length}?
                  <button className="btn sm ghost" onClick={() => setDiscardAsk(null)}>
                    Нет
                  </button>
                  <button
                    className="btn sm danger"
                    onClick={() => {
                      setDiscardAsk(null);
                      void gitOp(api.discard(sid, files.flatMap(pathsOf)));
                    }}
                  >
                    Откатить всё
                  </button>
                </span>
              ) : (
                <span className="idx-acts">
                  {stagedN > 0 ? (
                    <button className="linkbtn" onClick={() => void gitOp(api.unstage(sid, []))}>
                      Unstage all
                    </button>
                  ) : null}
                  {stagedN < files.length && (
                    <button className="linkbtn" onClick={() => void gitOp(api.stage(sid, []))}>
                      Stage all
                    </button>
                  )}
                  <button className="linkbtn" onClick={() => setDiscardAsk(["*", "*", "*", ...files.map((f) => f.path)])}>
                    Откатить всё
                  </button>
                </span>
              )
            ) : (
              <span className="sub">клик в код — комментарий</span>
            )}
          </div>
        </>
      )}

      <div className="diff" ref={scroller}>
        {files.map((f, fi) => (
          <div key={f.path}>
            <div className="dfile" id={`df-${fi}`}>
              <span>{f.path}</span>
              <span className="nums">
                <span className="a">+{f.add}</span> <span className="d">−{f.del}</span>
              </span>
            </div>
            {f.binary && <div className="hh">Бинарный файл</div>}
            {(f.hunks ?? EMPTY).map((h, hi) => {
              const fm = form && !form.editId && form.fi === fi && form.hi === hi ? form : null;
              const lo = fm ? Math.min(fm.li, fm.liE ?? fm.li) : -1;
              const hiIdx = fm ? Math.max(fm.li, fm.liE ?? fm.li) : -1;
              return (
                <div key={hi}>
                  <div className="hh">{h.header}</div>
                  {h.lines.map((l, li) => {
                    const cls = l.t === "+" ? " add" : l.t === "-" ? " del" : "";
                    const ranged = fm && li >= lo && li <= hiIdx ? " ranged" : "";
                    const here = byRow.get(rowKey(f.path, l)) ?? EMPTY;
                    let formLoc = "";
                    let formDeleted = false;
                    if (fm && li === hiIdx) {
                      if (fm.liE !== null && fm.liE !== fm.li) {
                        const a = newLineOf(h, lo), b = newLineOf(h, hiIdx);
                        formLoc = locString(f.path, a.n, b.n, undefined);
                        formDeleted = a.deleted && b.deleted;
                      } else {
                        const a = newLineOf(h, fm.li);
                        formLoc = locString(f.path, a.n, undefined, fm.col ?? undefined);
                        formDeleted = a.deleted;
                      }
                    }
                    return (
                      <div key={li}>
                        <div className={"dl" + cls + ranged}>
                          <span className="o">{l.o || ""}</span>
                          <span className="n">{l.n || ""}</span>
                          <span className="s">{l.t === " " ? "" : l.t === "-" ? "−" : "+"}</span>
                          <span
                            className="c"
                            onClick={(e) => {
                              if (String(window.getSelection() ?? "").length) return;
                              openForm(fi, hi, li, colFromPoint(e.currentTarget, e.clientX, e.clientY) ?? firstCol(l.text), e.shiftKey);
                            }}
                            dangerouslySetInnerHTML={{ __html: html[fi][hi][li] }}
                          />
                          <button
                            className="plus"
                            title="Комментарий (Shift — диапазон)"
                            aria-label="Добавить комментарий к строке"
                            onClick={(e) => openForm(fi, hi, li, firstCol(l.text), e.shiftKey)}
                          >
                            +
                          </button>
                        </div>
                        {here.map((c) => commentBox(c, !!c.outdated))}
                        {fm && li === hiIdx && <div className="cbox form">{formBox(formLoc, formDeleted)}</div>}
                      </div>
                    );
                  })}
                </div>
              );
            })}
          </div>
        ))}
        {orphans.length > 0 && (
          <div className="orphans">
            <div className="hh">Замечания вне текущего диффа ({orphans.length})</div>
            {orphans.map((c) => commentBox(c, !!c.outdated))}
          </div>
        )}
      </div>

      {diff && diff.ahead > 0 && <PushBar sid={sid} ahead={diff.ahead} upstream={diff.upstream} onPushed={load} />}
      {index && files.length > 0 && <CommitBox key={sid} sid={sid} stagedN={stagedN} onCommitted={afterCommit} />}

      <div className="cfoot">
        {confirmCancel && drafts.length > 0 ? (
          <>
            <span className="cnt">
              Удалить {drafts.length} {plural(drafts.length, ["черновик", "черновика", "черновиков"])}?
            </span>
            <button className="btn ghost" onClick={() => setConfirmCancel(false)}>
              Оставить
            </button>
            <button className="btn danger" onClick={() => void discard(false)}>
              Удалить
            </button>
          </>
        ) : (
          <>
            <span className="cnt">
              {drafts.length ? `${drafts.length} ${plural(drafts.length, ["замечание", "замечания", "замечаний"])} в черновике` : "Замечаний нет"}
              {sentCount > 0 && (
                <button className="linkbtn" onClick={() => void discard(true)} title="Убрать отправленные замечания из диффа">
                  · убрать отправленные ({sentCount})
                </button>
              )}
            </span>
            <button className="btn" disabled={!drafts.length} onClick={() => setConfirmCancel(true)}>
              Cancel
            </button>
            {mr?.mr && (
              <button className="btn" disabled={!drafts.length || busy} onClick={() => void toMR()} title={`Опубликовать черновики в MR !${mr.mr.iid}`}>
                В MR
              </button>
            )}
            <button className="btn primary" disabled={!drafts.length || busy} onClick={() => void submit()}>
              {busy ? "Отправляю…" : "Submit"}
            </button>
          </>
        )}
      </div>
    </aside>
  );
}
