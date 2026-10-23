import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { api } from "../lib/api";
import { guard, openFile } from "../lib/actions";
import { toast, useUI, EMPTY } from "../lib/store";
import type { Entry, Session } from "../lib/types";
import { Menu } from "./Floating";
import { EditPopover, type EditTarget } from "./EditPopover";
import { FileIcon, FolderIcon } from "../lib/fileicons";
import { ChevronRight } from "lucide-react";

// Expanded folders and last listings per session survive switching tabs, so
// a project reopens instantly and refreshes in the background.
const expandedBySession = new Map<string, Set<string>>();
const dirsCache = new Map<string, Record<string, Entry[]>>();
const statusCache = new Map<string, Record<string, string>>();

export function FileTree(props: { session: Session }) {
  const sid = props.session.id;
  const gitTick = useUI((s) => s.gitTick);
  const fsTick = useUI((s) => s.fsTick);
  const showIgnored = useUI((s) => s.st?.data.settings.showIgnored ?? false);
  const editor = useUI((s) => s.st?.data.settings.editor ?? "");
  const selected = useUI((s) => {
    const v = (s.views[sid] ?? EMPTY)[s.activeView[sid] ?? 0];
    return v && v.type === "file" ? v.path : "";
  });
  const [dirs, setDirsRaw] = useState<Record<string, Entry[]>>(() => dirsCache.get(sid) ?? {});
  const [status, setStatusRaw] = useState<Record<string, string>>(() => statusCache.get(sid) ?? {});
  const setDirs = useCallback(
    (fn: (d: Record<string, Entry[]>) => Record<string, Entry[]>) =>
      setDirsRaw((d) => {
        const next = fn(d);
        dirsCache.set(sid, next);
        return next;
      }),
    [sid],
  );
  const setStatus = useCallback(
    (m: Record<string, string>) => {
      statusCache.set(sid, m);
      setStatusRaw(m);
    },
    [sid],
  );
  const [menu, setMenu] = useState<{ path: string; x: number; y: number } | null>(null);
  const [edit, setEdit] = useState<EditTarget | null>(null);
  const expanded = useRef<Set<string>>(new Set());

  if (!expandedBySession.has(sid)) expandedBySession.set(sid, new Set());
  expanded.current = expandedBySession.get(sid)!;

  const load = useCallback(
    async (rel: string) => {
      const es = await api.listDir(sid, rel).catch(() => null);
      setDirs((d) => ({ ...d, [rel]: es ?? [] }));
    },
    [sid, setDirs],
  );

  const refresh = useCallback(async () => {
    const rels = ["", ...expanded.current];
    const [results, st] = await Promise.all([
      Promise.all(rels.map((r) => api.listDir(sid, r).catch(() => null))),
      api.gitStatus(sid).catch(() => null),
    ]);
    setDirs((prev) => {
      const next: Record<string, Entry[]> = { ...prev };
      rels.forEach((r, i) => {
        if (results[i]) next[r] = results[i]!;
        else if (!next[r]) next[r] = EMPTY;
      });
      return next;
    });
    if (st) setStatus(st);
  }, [sid, setDirs, setStatus]);

  useEffect(() => {
    void refresh();
  }, [sid, refresh]);

  useEffect(() => {
    if (gitTick > 0 || fsTick > 0) void refresh();
  }, [gitTick, fsTick, refresh]);

  const toggle = (rel: string) => {
    const ex = expanded.current;
    if (ex.has(rel)) ex.delete(rel);
    else {
      ex.add(rel);
      if (!dirs[rel]) void load(rel);
    }
    setDirs((d) => ({ ...d }));
  };

  const dirty = (rel: string) => Object.keys(status).some((k) => k.startsWith(rel + "/"));

  const rows: ReactNode[] = [];
  const walk = (rel: string, depth: number) => {
    for (const e of dirs[rel] ?? EMPTY) {
      if (e.ignored && !showIgnored) continue;
      const pad = 8;
      const guides = Array.from({ length: depth }, (_, i) => <span key={i} className="guide" />);
      if (e.dir) {
        const open = expanded.current.has(e.path);
        rows.push(
          <div
            key={e.path}
            className={"node dir" + (e.ignored ? " ignored" : "")}
            role="treeitem"
            aria-expanded={open}
            tabIndex={0}
            style={{ paddingLeft: pad }}
            onClick={() => toggle(e.path)}
            onKeyDown={(k) => k.key === "Enter" && toggle(e.path)}
          >
            {guides}
            <ChevronRight className={"tw" + (open ? " open" : "")} size={13} strokeWidth={2} aria-hidden="true" />
            <FolderIcon open={open} />
            <span className="nm">{e.name}</span>
            {!open && dirty(e.path) && <span className="st dirmark">●</span>}
          </div>,
        );
        if (open) walk(e.path, depth + 1);
      } else {
        const st = status[e.path] ?? "";
        const openMenu = (x: number, y: number) => setMenu({ path: e.path, x, y });
        rows.push(
          <div
            key={e.path}
            className={"node file" + (selected === e.path ? " sel" : "") + (e.ignored ? " ignored" : "")}
            role="treeitem"
            tabIndex={0}
            style={{ paddingLeft: pad }}
            onClick={() => openFile(sid, e.path)}
            onContextMenu={(ev) => {
              ev.preventDefault();
              openMenu(ev.clientX, ev.clientY);
            }}
            onKeyDown={(k) => {
              if (k.key === "Enter") openFile(sid, e.path);
              if (k.key === "ContextMenu" || (k.key === "F10" && k.shiftKey)) {
                k.preventDefault();
                const r = (k.target as HTMLElement).getBoundingClientRect();
                openMenu(r.left + 40, r.bottom);
              }
            }}
          >
            {guides}
            <span className="tw" />
            <FileIcon name={e.name} />
            <span className="nm">{e.name}</span>
            <span className={"st " + st}>{st}</span>
            <button
              className="more"
              aria-label="Действия с файлом"
              title="Действия"
              onClick={(ev) => {
                ev.stopPropagation();
                const r = ev.currentTarget.getBoundingClientRect();
                openMenu(r.right - 8, r.bottom + 4);
              }}
            >
              ⋯
            </button>
          </div>,
        );
      }
    }
  };
  walk("", 0);

  return (
    <>
      <div className="tree" role="tree">
        {rows.length ? rows : <div className="empty">{dirs[""] ? "Папка пуста" : "Загрузка…"}</div>}
      </div>
      {menu && (
        <Menu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          items={[
            { label: "Edit…", hint: "промпт агенту", run: () => setEdit({ sessionId: sid, path: menu.path, x: menu.x, y: menu.y }) },
            { label: "Открыть превью", run: () => openFile(sid, menu.path) },
            "sep",
            {
              label: "Copy path",
              run: () =>
                void api.copy(menu.path).then((ok) => toast(ok ? "Скопировано: " + menu.path : "Не удалось скопировать")),
            },
            { label: "Reveal in Finder", run: () => void guard(api.reveal(sid, menu.path)) },
            { label: editor ? `Open in ${editor}` : "Открыть в приложении", run: () => void guard(api.openExternal(sid, menu.path, editor)) },
          ]}
        />
      )}
      {edit && <EditPopover target={edit} onClose={() => setEdit(null)} />}
    </>
  );
}
