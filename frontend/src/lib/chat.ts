// Turns the ACP event stream into chat items.
import type { ACPEvent, ACPPermission } from "./types";

export interface ToolItem {
  type: "tool";
  id: string;
  title: string;
  kind: string;
  status: string;
  texts: string[];
  diffs: { path: string; add: number; del: number }[];
  locations: { path: string; line?: number }[];
}

export type ChatItem =
  | { type: "user"; text: string }
  | { type: "agent"; text: string }
  | { type: "thought"; text: string }
  | ToolItem
  | { type: "plan"; entries: { content: string; status: string }[] }
  | { type: "perm"; perm: ACPPermission; chosen?: string; done: boolean }
  | { type: "error"; text: string }
  | { type: "stop"; reason: string };

type U = Record<string, unknown>;

function textOf(c: unknown): string {
  const o = c as U | null;
  if (!o) return "";
  if (o.type === "text" && typeof o.text === "string") return o.text;
  if (o.type === "resource_link" && typeof o.uri === "string") return String(o.name ?? o.uri);
  return "";
}

function lineCount(s: unknown): number {
  return typeof s === "string" && s ? s.split("\n").length : 0;
}

function applyTool(t: ToolItem, u: U): void {
  if (typeof u.title === "string" && u.title) t.title = u.title;
  if (typeof u.kind === "string") t.kind = u.kind;
  if (typeof u.status === "string") t.status = u.status;
  if (Array.isArray(u.content)) {
    t.texts = [];
    t.diffs = [];
    for (const c of u.content as U[]) {
      if (c.type === "content") {
        const x = textOf(c.content);
        if (x) t.texts.push(x);
      } else if (c.type === "diff" && typeof c.path === "string") {
        t.diffs.push({ path: c.path, add: lineCount(c.newText), del: lineCount(c.oldText) });
      }
    }
  }
  if (Array.isArray(u.locations)) {
    t.locations = (u.locations as U[]).filter((l) => typeof l.path === "string").map((l) => ({ path: l.path as string, line: typeof l.line === "number" ? l.line : undefined }));
  }
}

export function buildChat(events: ACPEvent[]): ChatItem[] {
  const items: ChatItem[] = [];
  const tools = new Map<string, ToolItem>();
  const perms = new Map<string, ChatItem & { type: "perm" }>();
  let plan: (ChatItem & { type: "plan" }) | null = null;
  const appendText = (type: "agent" | "thought" | "user", text: string) => {
    const last = items[items.length - 1];
    if (last && last.type === type) last.text += text;
    else items.push({ type, text });
  };
  for (const e of events) {
    switch (e.kind) {
      case "user":
        items.push({ type: "user", text: e.text ?? "" });
        plan = null;
        break;
      case "error":
        items.push({ type: "error", text: e.text ?? "" });
        break;
      case "stop":
        if (e.text && e.text !== "end_turn") items.push({ type: "stop", reason: e.text });
        break;
      case "permission":
        if (e.perm) {
          const it = { type: "perm" as const, perm: e.perm, done: false };
          perms.set(e.perm.id, it);
          items.push(it);
        }
        break;
      case "permission:done": {
        const it = e.perm && perms.get(e.perm.id);
        if (it) {
          it.done = true;
          it.chosen = e.perm!.chosen;
        }
        break;
      }
      case "update": {
        const u = (e.update ?? {}) as U;
        switch (u.sessionUpdate) {
          case "agent_message_chunk":
            appendText("agent", textOf(u.content));
            break;
          case "agent_thought_chunk":
            appendText("thought", textOf(u.content));
            break;
          case "user_message_chunk":
            appendText("user", textOf(u.content));
            break;
          case "tool_call": {
            const id = String(u.toolCallId ?? "");
            const t: ToolItem = { type: "tool", id, title: "", kind: "other", status: "pending", texts: [], diffs: [], locations: [] };
            applyTool(t, u);
            tools.set(id, t);
            items.push(t);
            break;
          }
          case "tool_call_update": {
            const t = tools.get(String(u.toolCallId ?? ""));
            if (t) applyTool(t, u);
            break;
          }
          case "plan": {
            const entries = Array.isArray(u.entries) ? (u.entries as U[]).map((x) => ({ content: String(x.content ?? ""), status: String(x.status ?? "pending") })) : [];
            // one plan per user turn, updated in place
            if (plan) plan.entries = entries;
            else {
              plan = { type: "plan", entries };
              items.push(plan);
            }
            break;
          }
        }
      }
    }
  }
  return items;
}

/** Splits markdown into text and fenced code parts. */
export function splitFences(md: string): { code: boolean; lang: string; text: string }[] {
  const out: { code: boolean; lang: string; text: string }[] = [];
  const re = /```([\w+-]*)\n([\s\S]*?)(?:```|$)/g;
  let last = 0;
  for (let m = re.exec(md); m; m = re.exec(md)) {
    if (m.index > last) out.push({ code: false, lang: "", text: md.slice(last, m.index) });
    out.push({ code: true, lang: m[1], text: m[2].replace(/\n$/, "") });
    last = re.lastIndex;
  }
  if (last < md.length) out.push({ code: false, lang: "", text: md.slice(last) });
  // blank lines around code blocks are spacing the blocks already have
  return out
    .map((p, i) => {
      if (p.code) return p;
      let t = p.text;
      if (i > 0) t = t.replace(/^\n+/, "");
      if (i < out.length - 1) t = t.replace(/\n+$/, "");
      return { ...p, text: t };
    })
    .filter((p) => p.code || p.text !== "");
}
