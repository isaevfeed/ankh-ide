import { useEffect, useReducer, useRef } from "react";
import { attach, fitNow, getTerm, markRestarted } from "../lib/terminals";
import { api, errText } from "../lib/api";
import { toast } from "../lib/store";

/** Shows one live terminal. For the agent, starts it on first show. */
export function TerminalView(props: { termId: string; sessionId: string; kind: "agent" | "shell"; visible: boolean }) {
  const box = useRef<HTMLDivElement>(null);
  const [, rerender] = useReducer((x: number) => x + 1, 0);
  const entry = getTerm(props.termId);

  useEffect(() => {
    entry.listeners.add(rerender);
    return () => {
      entry.listeners.delete(rerender);
    };
  }, [entry]);

  useEffect(() => {
    if (!props.visible || !box.current) return;
    attach(entry, box.current);
    if (props.kind === "agent") {
      api
        .startAgent(props.sessionId, entry.term.cols, entry.term.rows)
        .then(() => api.termResize(props.termId, entry.term.cols, entry.term.rows).catch(() => {}))
        .catch((e) => toast("Агент не запустился: " + errText(e)));
    }
    entry.term.focus();
    // Refit once the size settles: every refit makes the agent's TUI redraw,
    // so fitting on each animation frame of a layout change makes it flicker.
    let timer = 0;
    const ro = new ResizeObserver(() => {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => fitNow(entry), 120);
    });
    ro.observe(box.current);
    return () => {
      window.clearTimeout(timer);
      ro.disconnect();
    };
  }, [props.visible, props.termId, props.sessionId, props.kind, entry]);

  const restart = async (fresh: boolean) => {
    try {
      if (props.kind === "agent") {
        await api.restartAgent(props.sessionId, entry.term.cols, entry.term.rows, fresh);
        markRestarted(props.termId);
        entry.term.focus();
      }
    } catch (e) {
      toast(errText(e));
    }
  };

  return (
    <div className="term-wrap" style={{ display: props.visible ? "flex" : "none" }}>
      <div className="term-box" ref={box} />
      {entry.exited !== null && (
        <div className="term-exit">
          <span>
            {props.kind === "agent" ? "Агент завершил работу" : "Shell закрыт"}
            {entry.exited !== 0 ? ` (код ${entry.exited})` : ""}
          </span>
          {props.kind === "agent" && (
            <>
              <button className="btn sm primary" onClick={() => void restart(false)}>
                Продолжить разговор
              </button>
              <button className="btn sm" onClick={() => void restart(true)}>
                Новый разговор
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
