import { api, errText } from "../lib/api";
import { activeSession, toast, useUI } from "../lib/store";
import type { UsageWindow } from "../lib/types";

const DAYS = ["вс", "пн", "вт", "ср", "чт", "пт", "сб"];
const hm = (d: Date) => d.toTimeString().slice(0, 5);

function resetText(w: UsageWindow): string {
  const unix = w.resetsAt;
  if (!unix) return w.resetsText ?? "";
  const d = new Date(unix * 1000);
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  return sameDay ? `в ${hm(d)}` : `${DAYS[d.getDay()]} ${hm(d)}`;
}

function Meter(props: { label: string; w?: UsageWindow; title: string }) {
  if (!props.w) return null;
  const v = Math.round(props.w.usedPercentage);
  return (
    <span className={"meter" + (v >= 90 ? " crit" : v >= 75 ? " hot" : "")} title={props.title}>
      <span>{props.label}</span>
      <span className="bar">
        <i style={{ width: `${Math.min(100, Math.max(0, v))}%` }} />
      </span>
      <b>{v}%</b>
      {resetText(props.w) && <span>сброс {resetText(props.w)}</span>}
    </span>
  );
}

export function UsageBar() {
  const usage = useUI((s) => s.usage);
  const missing = useUI((s) => s.usageMissing);
  const session = useUI(activeSession);
  const ctx = useUI((s) => (session ? s.context[session.id] : undefined));

  const request = async () => {
    if (!session) return;
    try {
      const r = await api.requestUsage(session.id);
      if (r === "queued") toast("Агент работает — /usage выполнится, когда он ответит");
    } catch (e) {
      toast(errText(e));
    }
  };

  return (
    <footer className="statusbar" aria-label="Лимиты">
      {ctx && (
        <span className={"meter ctx" + (ctx.usedPercentage >= 80 ? " crit" : ctx.usedPercentage >= 60 ? " hot" : "")} title={`Контекст этой сессии: ${Math.round(ctx.tokens / 1000)}k из ${Math.round(ctx.window / 1000)}k токенов`}>
          <span>Контекст</span>
          <span className="bar">
            <i style={{ width: `${Math.min(100, ctx.usedPercentage)}%` }} />
          </span>
          <b>{Math.round(ctx.usedPercentage)}%</b>
        </span>
      )}
      {usage ? (
        <>
          <Meter label="5 ч" w={usage.fiveHour} title="Текущее 5-часовое окно" />
          <Meter label="Неделя" w={usage.sevenDay} title="Недельный лимит, все модели" />
          <span className="upd" title="Claude Code присылает лимиты через statusLine после каждого ответа">
            обновлено {hm(new Date(usage.at * 1000))}
            {usage.source === "usage" ? " · через /usage" : ""}
            {usage.model ? ` · ${usage.model}` : ""}
          </span>
        </>
      ) : (
        <span className="upd" style={{ marginLeft: 0 }}>
          {missing
            ? "Claude Code не передаёт лимиты в statusLine — нажмите ↻ /usage"
            : "Лимиты появятся после первого ответа Claude Code"}
        </span>
      )}
      <button className="sb-btn" style={usage ? undefined : { marginLeft: "auto" }} onClick={() => void request()} title="Выполнить /usage в активной сессии">
        ↻ /usage
      </button>
    </footer>
  );
}
