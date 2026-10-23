import { useCallback, useEffect, useState } from "react";
import { ExternalLink, GitPullRequest, RefreshCw } from "lucide-react";
import { api, errText } from "../lib/api";
import { guard, reload } from "../lib/actions";
import { toast, useUI } from "../lib/store";
import type { MRInfo } from "../lib/types";
import { Menu } from "./Floating";

// Last MR status per session: shown at once, refreshed in the background.
const mrCache = new Map<string, MRInfo | null>();

const PIPE: Record<string, string> = { success: "pipeline ✓", failed: "pipeline ✕", running: "pipeline …", pending: "pipeline …", canceled: "pipeline отменён" };

/** The branch's GitLab merge request, via the user's glab CLI. */
export function MRBar(props: { sessionId: string; onInfo?: (i: MRInfo | null) => void }) {
  const sid = props.sessionId;
  const glab = useUI((s) => s.st?.glab ?? "");
  const head = useUI((s) => s.gitTick);
  const [info, setInfo] = useState<MRInfo | null>(() => mrCache.get(sid) ?? null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const onInfo = props.onInfo;

  const load = useCallback(async () => {
    try {
      const i = await api.mrStatus(sid);
      mrCache.set(sid, i);
      setInfo(i);
      setErr("");
      onInfo?.(i);
    } catch (e) {
      setErr(errText(e));
      onInfo?.(null);
    }
  }, [sid, onInfo]);

  useEffect(() => {
    if (glab) void load();
  }, [glab, load]);

  // HEAD moves (commit, push from a shell): refresh, but not more than once in 20 s.
  const [lastHead, setLastHead] = useState(0);
  useEffect(() => {
    if (!glab || head === 0) return;
    const now = Date.now();
    if (now - lastHead < 20_000) return;
    setLastHead(now);
    void load();
  }, [head]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!glab) return null;

  const create = async (draft: boolean) => {
    setBusy(true);
    try {
      const i = await api.mrCreate(sid, draft);
      mrCache.set(sid, i);
      setInfo(i);
      onInfo?.(i);
      if (i.mr) toast(`Создан MR !${i.mr.iid}`, { label: "Открыть", run: () => void api.openURL(i.mr!.web_url) });
    } catch (e) {
      toast(errText(e));
    } finally {
      setBusy(false);
    }
  };

  const importNotes = async () => {
    setBusy(true);
    const n = await guard(api.mrImport(sid));
    setBusy(false);
    if (n === undefined) return;
    await reload();
    toast(n ? `Из MR добавлено замечаний: ${n} — проверьте и нажмите Submit` : "Новых обсуждений на строках нет");
  };

  const mr = info?.mr;
  return (
    <div className="mrbar">
      <GitPullRequest size={14} className="mr-ic" />
      {err ? (
        <span className="mr-err" title={err}>
          {err.split("\n")[0]}
        </span>
      ) : !info ? (
        <span className="sub">MR…</span>
      ) : mr ? (
        <>
          <button className="linkbtn mr-title" title={mr.web_url} onClick={() => void api.openURL(mr.web_url)}>
            !{mr.iid} {mr.draft ? "Draft: " : ""}
            {mr.title.replace(/^(Draft|WIP):\s*/i, "")}
            <ExternalLink size={11} />
          </button>
          {mr.head_pipeline && <span className={"badge pipe " + mr.head_pipeline.status}>{PIPE[mr.head_pipeline.status] ?? mr.head_pipeline.status}</span>}
          {!info.pushed && <span className="badge" title="Локальный HEAD отличается от MR — замечания уйдут общим комментарием">не запушено</span>}
          {mr.unresolved > 0 && (
            <button className="btn sm" disabled={busy} onClick={() => void importNotes()} title="Добавить открытые обсуждения MR в черновики ревью">
              Забрать {mr.unresolved}
            </button>
          )}
        </>
      ) : (
        <>
          <span className="sub">Нет MR для {info.branch}</span>
          <button
            className="btn sm"
            disabled={busy}
            onClick={(e) => {
              const r = e.currentTarget.getBoundingClientRect();
              setMenu({ x: r.left, y: r.bottom + 4 });
            }}
          >
            {busy ? "Создаю…" : `Создать MR → ${info.base || "base"}`}
          </button>
        </>
      )}
      <span className="grow" />
      <button className="icon-btn sm" title="Обновить" aria-label="Обновить MR" onClick={() => void load()}>
        <RefreshCw size={13} />
      </button>
      {menu && (
        <Menu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          items={[
            { label: "Запушить и создать MR", hint: "glab mr create --fill", run: () => void create(false) },
            { label: "Создать черновой MR", hint: "--draft", run: () => void create(true) },
          ]}
        />
      )}
    </div>
  );
}
