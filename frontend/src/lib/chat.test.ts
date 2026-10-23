import { test } from "node:test";
import assert from "node:assert/strict";
import { buildChat, splitFences } from "./chat.ts";
import type { ACPEvent } from "./types.ts";

const up = (u: Record<string, unknown>): ACPEvent => ({ kind: "update", update: u as ACPEvent["update"] });

test("chunks, tools and permissions", () => {
  const items = buildChat([
    { kind: "user", text: "fix it" },
    up({ sessionUpdate: "agent_thought_chunk", content: { type: "text", text: "hmm" } }),
    up({ sessionUpdate: "agent_message_chunk", content: { type: "text", text: "Sure, " } }),
    up({ sessionUpdate: "agent_message_chunk", content: { type: "text", text: "editing." } }),
    up({ sessionUpdate: "tool_call", toolCallId: "t1", title: "Edit a.go", kind: "edit", status: "pending" }),
    { kind: "permission", perm: { id: "1", options: [{ optionId: "allow", name: "Allow", kind: "allow_once" }] } },
    { kind: "permission:done", perm: { id: "1", chosen: "allow" } },
    up({ sessionUpdate: "tool_call_update", toolCallId: "t1", status: "completed", content: [{ type: "diff", path: "/p/a.go", oldText: "a\nb", newText: "a\nb\nc" }] }),
    up({ sessionUpdate: "plan", entries: [{ content: "step", status: "in_progress" }] }),
    up({ sessionUpdate: "plan", entries: [{ content: "step", status: "completed" }] }),
    { kind: "stop", text: "end_turn" },
  ]);
  assert.deepEqual(
    items.map((i) => i.type),
    ["user", "thought", "agent", "tool", "perm", "plan"],
  );
  const agent = items[2] as { text: string };
  assert.equal(agent.text, "Sure, editing.");
  const tool = items[3] as { status: string; diffs: { add: number; del: number }[] };
  assert.equal(tool.status, "completed");
  assert.deepEqual(tool.diffs[0], { path: "/p/a.go", add: 3, del: 2 });
  const perm = items[4] as { done: boolean; chosen?: string };
  assert.equal(perm.done, true);
  assert.equal(perm.chosen, "allow");
  assert.equal((items[5] as { entries: { status: string }[] }).entries[0].status, "completed");
});

test("fences", () => {
  const parts = splitFences("a\n```go\nx := 1\n```\nb");
  assert.deepEqual(parts.map((p) => [p.code, p.lang, p.text]), [
    [false, "", "a"],
    [true, "go", "x := 1"],
    [false, "", "b"],
  ]);
});
