import { test } from "node:test";
import assert from "node:assert/strict";
import { buildRegex, findRanges, markHtml, markText } from "./search.ts";

test("regex options", () => {
  assert.deepEqual(findRanges("Foo foo food", buildRegex("foo", { case: false, word: false, regex: false })), [[0, 3], [4, 7], [8, 11]]);
  assert.deepEqual(findRanges("Foo foo food", buildRegex("foo", { case: true, word: true, regex: false })), [[4, 7]]);
  assert.deepEqual(findRanges("a.b axb", buildRegex("a.b", { case: false, word: false, regex: false })), [[0, 3]]);
  assert.deepEqual(findRanges("a.b axb", buildRegex("a.b", { case: false, word: false, regex: true })), [[0, 3], [4, 7]]);
  assert.equal(buildRegex("(", { case: false, word: false, regex: true }), null);
  assert.deepEqual(findRanges("xx", buildRegex("y*", { case: false, word: false, regex: true })), []);
});

test("markHtml keeps markup valid across tags and entities", () => {
  const html = '<span class="k">if</span> a &lt; b';
  // text: "if a < b" — mark "f a <"
  const out = markHtml(html, [[1, 6]], () => "m");
  assert.equal(out, '<span class="k">i<mark class="m">f</mark></span><mark class="m"> a &lt;</mark> b');
});

test("markText escapes", () => {
  assert.equal(markText("<a>", [[1, 2]]), '&lt;<mark class="">a</mark>&gt;');
});
