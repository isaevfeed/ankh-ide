import { test } from "node:test";
import assert from "node:assert/strict";
import { fuzzyFilter, fuzzyMatch } from "./fuzzy.ts";

const files = [
  "internal/api/handler.go",
  "internal/api/handler_test.go",
  "internal/app/service.go",
  "frontend/src/components/Changes.tsx",
  "frontend/src/lib/store.ts",
  "README.md",
  "docs/handbook/handling.md",
];

test("subsequence required", () => {
  assert.equal(fuzzyMatch("xyz", "internal/app/service.go"), null);
  assert.ok(fuzzyMatch("isvc", "internal/app/service.go"));
});

test("file name beats directories", () => {
  assert.equal(fuzzyFilter("handler", files)[0].path, "internal/api/handler.go");
  assert.equal(fuzzyFilter("store", files)[0].path, "frontend/src/lib/store.ts");
  assert.equal(fuzzyFilter("chg", files)[0].path, "frontend/src/components/Changes.tsx");
});

test("path segments help", () => {
  assert.equal(fuzzyFilter("app/serv", files)[0].path, "internal/app/service.go");
});

test("hits point at matched characters", () => {
  const m = fuzzyMatch("rdm", "README.md")!;
  assert.equal(m.hits.length, 3);
  assert.equal("README.md"[m.hits[0]].toLowerCase(), "r");
});

test("empty query lists recent first", () => {
  const r = fuzzyFilter("", files, 3, ["README.md"]);
  assert.equal(r[0].path, "README.md");
  assert.equal(r.length, 3);
});
