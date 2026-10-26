// Pure helpers for turning a clicked diff row into a review comment.
import type { Comment, DiffLine, Hunk } from "./types";

/** New-side line number for a row; deleted rows borrow the nearest new line. */
export function newLineOf(h: Hunk, idx: number): { n: number; deleted: boolean } {
  const ls = h.lines;
  if (ls[idx].t !== "-") return { n: ls[idx].n, deleted: false };
  for (let j = idx + 1; j < ls.length; j++) if (ls[j].t !== "-") return { n: ls[j].n, deleted: true };
  for (let j = idx - 1; j >= 0; j--) if (ls[j].t !== "-") return { n: ls[j].n, deleted: true };
  return { n: 1, deleted: true };
}

export function locString(path: string, a: number, b: number | undefined, col: number | undefined): string {
  if (b !== undefined && b !== a) return `${path}:${Math.min(a, b)}-${Math.max(a, b)}`;
  if (col) return `${path}:${a}:${col}`;
  return `${path}:${a}`;
}

export function firstCol(text: string): number {
  const m = text.match(/\S/);
  return m && m.index !== undefined ? m.index + 1 : 1;
}

/** Builds the comment to save for a row (or a range of rows) of one hunk. */
export function draftComment(path: string, h: Hunk, from: number, to: number | null, col: number | null, text: string): Partial<Comment> {
  const row = h.lines[from];
  if (to === null || to === from) {
    const nl = newLineOf(h, from);
    const isOld = row.t === "-";
    return {
      path,
      side: isOld ? "old" : "new",
      line: isOld ? row.o : row.n,
      col: col ?? undefined,
      loc: locString(path, nl.n, undefined, col ?? undefined),
      anchor: row.text,
      deleted: nl.deleted,
      text,
    };
  }
  const lo = Math.min(from, to);
  const hi = Math.max(from, to);
  const a = newLineOf(h, lo);
  const b = newLineOf(h, hi);
  return {
    path,
    side: "new",
    line: a.n,
    lineEnd: b.n,
    loc: locString(path, a.n, b.n, undefined),
    anchor: h.lines[hi].text,
    deleted: a.deleted && b.deleted,
    text,
  };
}

/** Key of the row a comment hangs under; equals rowKey of that row (see anchoredHere). */
export function commentKey(c: Comment): string {
  return `${c.path}\0${c.side === "old" ? "o" : "n"}\0${c.lineEnd || c.line}`;
}

export function rowKey(path: string, l: DiffLine): string {
  return l.t === "-" ? `${path}\0o\0${l.o}` : `${path}\0n\0${l.n}`;
}

/** Does this comment hang under this row of this file? (rendered after its last line) */
export function anchoredHere(c: Comment, path: string, l: DiffLine): boolean {
  if (c.path !== path) return false;
  const end = c.lineEnd || c.line;
  if (c.side === "old") return l.t === "-" && l.o === end;
  return l.t !== "-" && l.n === end;
}
