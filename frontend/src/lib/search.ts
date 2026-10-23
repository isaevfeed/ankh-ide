// Text search helpers shared by find-in-file (⌘F) and project search (⌘⇧F).

export interface SearchOpts {
  case: boolean;
  word: boolean;
  regex: boolean;
  include: string;
}

export const defaultOpts: SearchOpts = { case: false, word: false, regex: false, include: "" };

const escapeRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/** The same matcher the backend uses; null for an empty or invalid query. */
export function buildRegex(q: string, o: Pick<SearchOpts, "case" | "word" | "regex">): RegExp | null {
  if (!q) return null;
  let src = o.regex ? q : escapeRe(q);
  if (o.word) src = `\\b(?:${src})\\b`;
  try {
    return new RegExp(src, o.case ? "gu" : "giu");
  } catch {
    return null;
  }
}

/** [start, end) ranges of every match in text (empty matches skipped). */
export function findRanges(text: string, re: RegExp | null): [number, number][] {
  if (!re) return [];
  const out: [number, number][] = [];
  re.lastIndex = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m[0].length === 0) {
      re.lastIndex++;
      continue;
    }
    out.push([m.index, m.index + m[0].length]);
    if (out.length > 500) break;
  }
  return out;
}

const esc = (s: string) => s.replace(/[&<>"]/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[ch]!);

/** Plain text with <mark> around ranges. */
export function markText(text: string, ranges: [number, number][], cls = (i: number) => (i >= 0 ? "" : "")): string {
  let out = "";
  let at = 0;
  ranges.forEach(([a, b], i) => {
    out += esc(text.slice(at, a)) + `<mark class="${cls(i)}">` + esc(text.slice(a, b)) + "</mark>";
    at = b;
  });
  return out + esc(text.slice(at));
}

/**
 * Wraps text ranges of already highlighted HTML (highlight.js output: spans and
 * entities) in <mark>, closing and reopening the mark around tags so the
 * markup stays valid. Offsets count decoded characters.
 */
export function markHtml(html: string, ranges: [number, number][], cls: (i: number) => string): string {
  if (!ranges.length) return html;
  let out = "";
  let pos = 0; // text offset
  let r = 0; // current range
  let open = false;
  const openMark = () => {
    out += `<mark class="${cls(r)}">`;
    open = true;
  };
  const closeMark = () => {
    out += "</mark>";
    open = false;
  };
  for (let i = 0; i < html.length; ) {
    if (html[i] === "<") {
      const end = html.indexOf(">", i);
      const tag = html.slice(i, end < 0 ? html.length : end + 1);
      if (open) {
        closeMark();
        out += tag;
        openMark();
      } else out += tag;
      i += tag.length;
      continue;
    }
    while (r < ranges.length && ranges[r][1] <= pos) r++;
    if (!open && r < ranges.length && pos === ranges[r][0]) openMark();
    let ch = html[i];
    if (ch === "&") {
      const semi = html.indexOf(";", i);
      if (semi > i && semi - i < 10) ch = html.slice(i, semi + 1);
    }
    out += ch;
    i += ch.length;
    pos++;
    if (open && pos === ranges[r][1]) {
      closeMark();
      r++;
      if (r < ranges.length && pos === ranges[r][0]) openMark();
    }
  }
  if (open) closeMark();
  return out.replace(/<mark class="[^"]*"><\/mark>/g, "");
}
