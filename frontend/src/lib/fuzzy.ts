// Fuzzy file-name matching for quick open (⌘P).

export interface Match {
  path: string;
  score: number;
  hits: number[]; // matched character positions in path
}

const isSep = (c: string) => c === "/" || c === "." || c === "_" || c === "-" || c === " ";

function bonusAt(path: string, i: number, base: number): number {
  if (i === base) return 8; // first letter of the file name
  if (i === 0) return 5;
  const prev = path[i - 1];
  if (isSep(prev)) return 5;
  if (prev >= "a" && prev <= "z" && path[i] >= "A" && path[i] <= "Z") return 4; // camelCase
  return 0;
}

function scoreHits(path: string, hits: number[], base: number): number {
  let sc = 0;
  for (let k = 0; k < hits.length; k++) {
    const i = hits[k];
    sc += 1 + bonusAt(path, i, base);
    if (k > 0 && hits[k - 1] === i - 1) sc += 6;
    else if (k > 0) sc -= Math.min(3, (i - hits[k - 1]) / 8);
    if (i >= base) sc += 2; // inside the file name
  }
  return sc;
}

function forward(lp: string, q: string, from: number): number[] | null {
  const hits: number[] = [];
  let i = from;
  for (const ch of q) {
    i = lp.indexOf(ch, i);
    if (i < 0) return null;
    hits.push(i++);
  }
  return hits;
}

function backward(lp: string, q: string): number[] | null {
  const hits: number[] = [];
  let i = lp.length - 1;
  for (let k = q.length - 1; k >= 0; k--) {
    i = lp.lastIndexOf(q[k], i);
    if (i < 0) return null;
    hits.unshift(i--);
  }
  return hits;
}

/** Scores path against query; null when not every query character appears in order. */
export function fuzzyMatch(query: string, path: string): Match | null {
  const q = query.toLowerCase().replace(/\s+/g, "");
  if (!q) return { path, score: 0, hits: [] };
  const lp = path.toLowerCase();
  const base = lp.lastIndexOf("/") + 1;
  const cands = [forward(lp, q, 0), backward(lp, q), forward(lp, q, base)].filter((h): h is number[] => h !== null);
  if (!cands.length) return null;
  let best = cands[0];
  let bestScore = scoreHits(path, best, base);
  for (const h of cands.slice(1)) {
    const sc = scoreHits(path, h, base);
    if (sc > bestScore) [best, bestScore] = [h, sc];
  }
  const name = lp.slice(base);
  if (name.startsWith(q)) bestScore += 30;
  else if (name.includes(q)) bestScore += 15;
  else if (lp.includes(q)) bestScore += 8;
  bestScore -= path.length / 40;
  return { path, score: bestScore, hits: best };
}

/** Best matches first; recent paths win ties and empty queries. */
export function fuzzyFilter(query: string, paths: string[], limit = 60, recent: string[] = []): Match[] {
  const rank = new Map(recent.map((p, i) => [p, recent.length - i]));
  if (!query.trim()) {
    const rec = recent.filter((p) => paths.includes(p)).map((p) => ({ path: p, score: 0, hits: [] }));
    const rest = paths.filter((p) => !rank.has(p)).slice(0, Math.max(0, limit - rec.length)).map((p) => ({ path: p, score: 0, hits: [] }));
    return [...rec, ...rest].slice(0, limit);
  }
  const out: Match[] = [];
  for (const p of paths) {
    const m = fuzzyMatch(query, p);
    if (m) {
      m.score += (rank.get(p) ?? 0) * 0.5;
      out.push(m);
    }
  }
  out.sort((a, b) => b.score - a.score || a.path.length - b.path.length);
  return out.slice(0, limit);
}
