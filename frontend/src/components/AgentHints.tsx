import { Eraser, Minimize2, X } from "lucide-react";
import { dismissHint, runAgentCommand } from "../lib/actions";
import { useUI } from "../lib/store";

// Context thresholds: past SOFT suggest /compact gently, past HARD insist.
const SOFT = 60;
const HARD = 80;

/** Suggests /compact when the context fills up and /clear when a task is done. */
export function AgentHints(props: { sessionId: string; claude: boolean }) {
  const sid = props.sessionId;
  const ctx = useUI((s) => s.context[sid]);
  const clearReason = useUI((s) => s.clearHint[sid] ?? "");
  const dismissed = useUI((s) => s.hintDismissed[sid] ?? "");
  if (!props.claude) return null;

  const pct = ctx ? Math.round(ctx.usedPercentage) : 0;
  const level = pct >= HARD ? "hard" : pct >= SOFT ? "soft" : "";
  const compactKey = `compact-${level}`;
  const clearKey = `clear-${clearReason}`;

  if (clearReason && dismissed !== clearKey) {
    return (
      <div className="hint">
        <Eraser size={15} strokeWidth={1.8} />
        <span>
          <b>Можно начать с чистого листа.</b> {clearReason[0].toUpperCase() + clearReason.slice(1)}
          {pct ? `, контекст занят на ${pct}%` : ""}. /clear сбросит историю разговора, файлы не трогает.
        </span>
        <button className="btn sm primary" onClick={() => void runAgentCommand(sid, "/clear")}>
          /clear
        </button>
        <button className="icon-btn" aria-label="Скрыть" onClick={() => dismissHint(sid, clearKey)}>
          <X size={14} />
        </button>
      </div>
    );
  }
  if (level && dismissed !== compactKey) {
    return (
      <div className={"hint " + level}>
        <Minimize2 size={15} strokeWidth={1.8} />
        <span>
          <b>Контекст занят на {pct}%.</b>{" "}
          {level === "hard"
            ? "Сделайте /compact сейчас, иначе агент скоро начнёт терять детали."
            : "Скоро понадобится /compact: агент сожмёт историю и продолжит с кратким пересказом."}
        </span>
        <button className="btn sm primary" onClick={() => void runAgentCommand(sid, "/compact")}>
          /compact
        </button>
        <button className="icon-btn" aria-label="Скрыть" onClick={() => dismissHint(sid, compactKey)}>
          <X size={14} />
        </button>
      </div>
    );
  }
  return null;
}
