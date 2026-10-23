// Syntax highlighting with highlight.js (only the languages we register are bundled).
import hljs from "highlight.js/lib/core";
import go from "highlight.js/lib/languages/go";
import typescript from "highlight.js/lib/languages/typescript";
import javascript from "highlight.js/lib/languages/javascript";
import python from "highlight.js/lib/languages/python";
import sql from "highlight.js/lib/languages/sql";
import bash from "highlight.js/lib/languages/bash";
import json from "highlight.js/lib/languages/json";
import yaml from "highlight.js/lib/languages/yaml";
import ini from "highlight.js/lib/languages/ini";
import markdown from "highlight.js/lib/languages/markdown";
import css from "highlight.js/lib/languages/css";
import scss from "highlight.js/lib/languages/scss";
import xml from "highlight.js/lib/languages/xml";
import dockerfile from "highlight.js/lib/languages/dockerfile";
import makefile from "highlight.js/lib/languages/makefile";
import rust from "highlight.js/lib/languages/rust";
import java from "highlight.js/lib/languages/java";
import kotlin from "highlight.js/lib/languages/kotlin";
import php from "highlight.js/lib/languages/php";
import ruby from "highlight.js/lib/languages/ruby";
import c from "highlight.js/lib/languages/c";
import cpp from "highlight.js/lib/languages/cpp";
import protobuf from "highlight.js/lib/languages/protobuf";
import graphql from "highlight.js/lib/languages/graphql";
import swift from "highlight.js/lib/languages/swift";
import lua from "highlight.js/lib/languages/lua";
import plaintext from "highlight.js/lib/languages/plaintext";

const langs = { go, typescript, javascript, python, sql, bash, json, yaml, ini, markdown, css, scss, xml, dockerfile, makefile, rust, java, kotlin, php, ruby, c, cpp, protobuf, graphql, swift, lua, plaintext };
for (const [name, fn] of Object.entries(langs)) hljs.registerLanguage(name, fn);

const BY_EXT: Record<string, string> = {
  go: "go", mod: "go", sum: "plaintext",
  ts: "typescript", tsx: "typescript", mts: "typescript", cts: "typescript",
  js: "javascript", jsx: "javascript", mjs: "javascript", cjs: "javascript",
  py: "python", sql: "sql",
  sh: "bash", bash: "bash", zsh: "bash", env: "bash",
  json: "json", jsonc: "json", lock: "json",
  yml: "yaml", yaml: "yaml", toml: "ini", ini: "ini", cfg: "ini", conf: "ini", properties: "ini",
  md: "markdown", markdown: "markdown",
  css: "css", scss: "scss", sass: "scss", less: "scss",
  html: "xml", htm: "xml", xml: "xml", svg: "xml", vue: "xml", plist: "xml",
  rs: "rust", java: "java", kt: "kotlin", kts: "kotlin", php: "php", rb: "ruby",
  c: "c", h: "c", cc: "cpp", cpp: "cpp", hpp: "cpp", proto: "protobuf",
  graphql: "graphql", gql: "graphql", swift: "swift", lua: "lua",
};

const BY_NAME: Record<string, string> = {
  makefile: "makefile", "gnumakefile": "makefile", dockerfile: "dockerfile", ".gitignore": "bash", ".dockerignore": "bash",
  ".env": "bash", ".zshrc": "bash", ".bashrc": "bash", "go.mod": "go", "go.sum": "plaintext",
};

export function langOf(path: string): string | null {
  const name = path.split("/").pop()!.toLowerCase();
  if (BY_NAME[name]) return BY_NAME[name];
  if (name.startsWith("dockerfile")) return "dockerfile";
  if (name.startsWith(".env")) return "bash";
  const ext = name.includes(".") ? name.split(".").pop()! : "";
  return BY_EXT[ext] ?? null;
}

const esc = (s: string) => s.replace(/[&<>"]/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[ch]!);

/** Highlights one line. Lines are highlighted on their own, so block
 * comments spanning lines are only partly coloured. */
export function highlight(text: string, path: string): string {
  const lang = langOf(path);
  if (!lang || lang === "plaintext" || text.length > 2000) return esc(text);
  try {
    return hljs.highlight(text, { language: lang, ignoreIllegals: true }).value;
  } catch {
    return esc(text);
  }
}

/** Highlights a whole file and splits the HTML into lines, closing and
 * reopening spans at line breaks so multi-line tokens stay coloured. */
export function highlightFile(content: string, path: string): string[] {
  const lang = langOf(path);
  const lines = content.replace(/\n$/, "").split("\n");
  if (!lang || lang === "plaintext" || content.length > 400_000) return lines.map(esc);
  let html: string;
  try {
    html = hljs.highlight(content.replace(/\n$/, ""), { language: lang, ignoreIllegals: true }).value;
  } catch {
    return lines.map(esc);
  }
  const out: string[] = [];
  const open: string[] = [];
  let cur = "";
  const re = /(<span[^>]*>)|(<\/span>)|(\n)|([^<\n]+|<)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(html))) {
    if (m[1]) {
      open.push(m[1]);
      cur += m[1];
    } else if (m[2]) {
      open.pop();
      cur += m[2];
    } else if (m[3]) {
      out.push(cur + "</span>".repeat(open.length));
      cur = open.join("");
    } else {
      cur += m[4];
    }
  }
  out.push(cur + "</span>".repeat(open.length));
  return out;
}

/** Highlights a fenced code block by its language tag (```go). */
export function highlightCode(code: string, lang: string): string {
  const l = lang.toLowerCase();
  const name = hljs.getLanguage(l) ? l : langOf("x." + l);
  if (!name || !hljs.getLanguage(name) || code.length > 100_000) return esc(code);
  try {
    return hljs.highlight(code, { language: name, ignoreIllegals: true }).value;
  } catch {
    return esc(code);
  }
}
