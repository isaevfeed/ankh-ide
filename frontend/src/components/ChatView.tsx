import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api } from "../lib/api";
import { guard, openFile } from "../lib/actions";
import { buildChat, splitFences, type ChatItem, type ToolItem } from "../lib/chat";
import { highlightCode } from "../lib/highlight";
import { setState, useUI } from "../lib/store";
import type { ACPInfo } from "../lib/types";

const EMPTY_CHAT: ACPInfo = { state: "", command: "", events: [] };

/** Inline markdown: `code` and **bold**; the rest keeps its line breaks. */
function inline(text: string): ReactNode[] {
  const out: ReactNode[] = [];
  const re = /`([^`\n]+)`|\*\*([^*\n]+)\*\*/g;
  let last = 0;
  let k = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m.index > last) out.push(text.slice(last, m.index));
    out.push(m[1] !== undefined ? <code key={k++}>{m[1]}</code> : <b key={k++}>{m[2]}</b>);
    last = re.lastIndex;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

function Markdown(props: { text: string }) {
  return (
    <>
      {splitFences(props.text).map((p, i) =>
        p.code ? (
          <pre key={i} className="md-code">
            <code dangerouslySetInnerHTML={{ __html: highlightCode(p.text, p.lang) }} />
          </pre>
        ) : (
          <div key={i} className="md-text">
            {p.text.split("\n").map((line, j) => {
              const h = line.match(/^(#{1,4})\s+(.*)$/);
              return (
                <div key={j} className={h ? "md-h" : undefined}>
                  {inline(h ? h[2] : line) }
                  {line === "" && " "}
                </div>
              );
            })}
          </div>
        ),
      )}
    </>
  );
}

const STATUS: Record<string, string> = { pending: "ждёт", in_progress: "выполняется", completed: "готово", failed: "ошибка" };

function Tool(props: { t: ToolItem; root: string; sessionId: string }) {
  const { t } = props;
  const [open, setOpen] = useState(false);
  const rel = (p: string) => (p.startsWith(props.root + "/") ? p.slice(props.root.length + 1) : p);
  return (
    <div className={"ch-tool " + t.status}>
      <button className="ch-tool-head" onClick={() => setOpen(!open)} aria-expanded={open}>
        <span className="ch-dot" />
        <span className="ch-tool-title">{t.title || t.kind}</span>
        {t.diffs.map((d) => (
          <span key={d.path} className="nums">
            <span className="a">+{d.add}</span> <span className="d">−{d.del}</span>
          </span>
        ))}
        <span className="ch-tool-st">{STATUS[t.status] ?? t.status}</span>
      </button>
      {open && (
        <div className="ch-tool-body">
          {t.locations.map((l, i) => (
            <button key={i} className="linkbtn mono" onClick={() => openFile(props.sessionId, rel(l.path))}>
              {rel(l.path)}
              {l.line ? `:${l.line}` : ""}
            </button>
          ))}
          {t.diffs.map((d) => (
            <button key={d.path} className="linkbtn mono" onClick={() => openFile(props.sessionId, rel(d.path))}>
              {rel(d.path)}
            </button>
          ))}
          {t.texts.map((x, i) => (
            <pre key={i} className="md-code small">
              {x.length > 4000 ? x.slice(0, 4000) + "\n…" : x}
            </pre>
          ))}
        </div>
      )}
    </div>
  );
}

function Item(props: { it: ChatItem; root: string; sessionId: string }) {
  const { it, sessionId } = props;
  switch (it.type) {
    case "user":
      return (
        <div className="ch-user">
          <Markdown text={it.text} />
        </div>
      );
    case "agent":
      return (
        <div className="ch-agent">
          <Markdown text={it.text} />
        </div>
      );
    case "thought":
      return (
        <details className="ch-thought">
          <summary>Размышления</summary>
          <Markdown text={it.text} />
        </details>
      );
    case "tool":
      return <Tool t={it} root={props.root} sessionId={sessionId} />;
    case "plan":
      return (
        <div className="ch-plan">
          <div className="label">План</div>
          {it.entries.map((e, i) => (
            <div key={i} className={"ch-step " + e.status}>
              <span className="ch-check">{e.status === "completed" ? "✓" : e.status === "in_progress" ? "›" : "·"}</span>
              <span className="ch-txt">{e.content}</span>
            </div>
          ))}
        </div>
      );
    case "perm": {
      const opts = it.perm.options ?? [];
      return (
        <div className={"ch-perm" + (it.done ? " done" : "")}>
          <div>
            Агент просит разрешение: <b>{it.perm.toolCall?.title ?? "действие"}</b>
          </div>
          {it.done ? (
            <div className="hintline">{it.chosen ? `Ответ: ${opts.find((o) => o.optionId === it.chosen)?.name ?? it.chosen}` : "Отклонено"}</div>
          ) : (
            <div className="ch-perm-acts">
              {opts.map((o) => (
                <button
                  key={o.optionId}
                  className={"btn sm" + (o.kind.startsWith("allow") ? " primary" : "")}
                  onClick={() => void guard(api.acpPermission(sessionId, it.perm.id, o.optionId))}
                >
                  {o.name}
                </button>
              ))}
              <button className="btn sm ghost" onClick={() => void guard(api.acpPermission(sessionId, it.perm.id, ""))}>
                Отклонить
              </button>
            </div>
          )}
        </div>
      );
    }
    case "error":
      return <div className="ch-error">{it.text}</div>;
    case "stop":
      return <div className="ch-stop">Остановлено: {it.reason === "cancelled" ? "отменено" : it.reason}</div>;
  }
}

/** Chat mode: the agent driven through ACP — messages, tool calls and
 * permission requests as UI instead of a terminal. */
export function ChatView(props: { sessionId: string; root: string }) {
  const sid = props.sessionId;
  const info = useUI((s) => s.acp[sid] ?? EMPTY_CHAT);
  const [text, setText] = useState("");
  const scroller = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const items = useMemo(() => buildChat(info.events), [info.events]);
  const working = info.state === "working";

  useEffect(() => {
    const el = scroller.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [items]);

  const send = async () => {
    const t = text.trim();
    if (!t) return;
    const ok = await guard(api.acpPrompt(sid, t).then(() => true));
    if (ok) setText("");
  };

  const restart = async () => {
    await guard(api.acpStop(sid));
    await guard(api.acpStart(sid));
  };

  const clear = async () => {
    await guard(api.acpClear(sid));
    setState((s) => ({ acp: { ...s.acp, [sid]: { ...EMPTY_CHAT, command: info.command } } }));
    await guard(api.acpStart(sid));
  };

  const stateText: Record<string, string> = { "": "не запущен", starting: "запускаю…", ready: "готов", working: "отвечает…", closed: "остановлен" };

  return (
    <div className="view chat">
      <div className="ch-head">
        <span className={"dot " + (working ? "working" : info.state === "ready" ? "idle" : "")} />
        <span className="sub">ACP · {stateText[info.state] ?? info.state}</span>
        <span className="mono ch-cmd" title={info.command}>
          {info.command}
        </span>
        <span className="grow" />
        {info.state === "closed" || info.state === "" ? (
          <button className="btn sm" onClick={() => void guard(api.acpStart(sid))}>
            Запустить
          </button>
        ) : (
          <button className="btn sm ghost" onClick={() => void restart()} title="Перезапустить агента, история чата сохранится">
            Перезапустить
          </button>
        )}
        <button className="btn sm ghost" onClick={() => void clear()} title="Новый разговор">
          Очистить
        </button>
      </div>
      <div
        className="ch-log"
        ref={scroller}
        onScroll={(e) => {
          const el = e.currentTarget;
          stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
        }}
      >
        {items.length === 0 && (
          <div className="empty">
            Структурный чат с агентом через Agent Client Protocol: сообщения, вызовы инструментов и запросы разрешений — отдельными блоками.
            <br />
            Edit и Submit отправляют сюда, пока открыта эта вкладка.
          </div>
        )}
        {items.map((it, i) => (
          <Item key={i} it={it} root={props.root} sessionId={sid} />
        ))}
        {info.error && <div className="ch-error">{info.error}</div>}
      </div>
      <div className="ch-input">
        <textarea
          className="field"
          value={text}
          placeholder={info.state === "ready" ? "Сообщение агенту… (Enter — отправить, Shift+Enter — новая строка)" : working ? "Агент отвечает… можно писать следующее сообщение" : "Агент не готов"}
          disabled={info.state !== "ready" && info.state !== "working"}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault();
              if (!working) void send();
            }
          }}
        />
        {working ? (
          <button className="btn" onClick={() => void guard(api.acpCancel(sid))}>
            Стоп
          </button>
        ) : (
          <button className="btn primary" disabled={!text.trim() || info.state !== "ready"} onClick={() => void send()}>
            Отправить
          </button>
        )}
      </div>
    </div>
  );
}
