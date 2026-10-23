import { test } from "node:test";
import assert from "node:assert/strict";
import { anchoredHere, draftComment, firstCol, locString, newLineOf } from "./review.ts";
import type { Hunk } from "./types.ts";

const h: Hunk = {
  header: "@@ -10,4 +10,5 @@",
  lines: [
    { t: " ", o: 10, n: 10, text: "\tif err != nil {" },
    { t: "-", o: 11, n: 0, text: "\t\treturn nil, err" },
    { t: "+", o: 0, n: 11, text: "\t\treturn nil, fmt.Errorf(\"x: %w\", err)" },
    { t: "+", o: 0, n: 12, text: "\t\t// note" },
    { t: " ", o: 12, n: 13, text: "\t}" },
  ],
};

test("newLineOf maps deleted rows to the next new line", () => {
  assert.deepEqual(newLineOf(h, 1), { n: 11, deleted: true });
  assert.deepEqual(newLineOf(h, 3), { n: 12, deleted: false });
});

test("locString", () => {
  assert.equal(locString("a.go", 5, undefined, 3), "a.go:5:3");
  assert.equal(locString("a.go", 9, 5, 3), "a.go:5-9");
  assert.equal(locString("a.go", 5, undefined, undefined), "a.go:5");
});

test("firstCol skips indentation", () => {
  assert.equal(firstCol("\t\treturn"), 3);
  assert.equal(firstCol(""), 1);
});

test("draftComment single, deleted and range", () => {
  const c = draftComment("a.go", h, 2, null, 10, "rename");
  assert.equal(c.loc, "a.go:11:10");
  assert.equal(c.side, "new");
  const d = draftComment("a.go", h, 1, null, 3, "why");
  assert.equal(d.side, "old");
  assert.equal(d.line, 11);
  assert.equal(d.loc, "a.go:11:3");
  assert.equal(d.deleted, true);
  const r = draftComment("a.go", h, 4, 2, null, "range");
  assert.equal(r.loc, "a.go:11-13");
  assert.equal(r.lineEnd, 13);
  assert.ok(anchoredHere(r as never, "a.go", h.lines[4]));
  assert.ok(anchoredHere(d as never, "a.go", h.lines[1]));
  assert.ok(!anchoredHere(d as never, "a.go", h.lines[2]));
  // same line number in another file: not here
  assert.ok(!anchoredHere(r as never, "b.go", h.lines[4]));
});
