// File and folder icons for the tree (lucide), coloured by kind.
import {
  BookOpen, Braces, Container, Database, File, FileCode, FileCog, FileLock, FileSpreadsheet,
  FileText, FileType, Folder, FolderOpen, Image, KeyRound, Package, Shield, Terminal, Wrench,
  type LucideIcon,
} from "lucide-react";

interface Kind {
  icon: LucideIcon;
  color: string; // CSS colour token
}

const CODE: Record<string, string> = {
  go: "var(--ic-go)", ts: "var(--ic-ts)", tsx: "var(--ic-ts)", js: "var(--ic-js)", jsx: "var(--ic-js)", mjs: "var(--ic-js)",
  py: "var(--ic-py)", rs: "var(--ic-rs)", java: "var(--ic-rs)", kt: "var(--ic-kt)", swift: "var(--ic-rs)", rb: "var(--ic-rs)",
  php: "var(--ic-kt)", c: "var(--ic-ts)", h: "var(--ic-ts)", cpp: "var(--ic-ts)", cc: "var(--ic-ts)", lua: "var(--ic-ts)",
  proto: "var(--ic-cfg)", graphql: "var(--ic-kt)", gql: "var(--ic-kt)", vue: "var(--ic-go)", html: "var(--ic-rs)",
  css: "var(--ic-ts)", scss: "var(--ic-kt)",
};

export function fileKind(name: string): Kind {
  const n = name.toLowerCase();
  const ext = n.includes(".") ? n.split(".").pop()! : "";
  if (n === "dockerfile" || n.startsWith("dockerfile.") || n === "docker-compose.yml" || n === "compose.yaml") return { icon: Container, color: "var(--ic-ts)" };
  if (n === "makefile" || ext === "mk") return { icon: Wrench, color: "var(--ic-cfg)" };
  if (n === "go.mod" || n === "package.json" || n === "cargo.toml" || n === "pyproject.toml") return { icon: Package, color: "var(--ic-go)" };
  if (n === "go.sum" || ext === "lock" || n === "package-lock.json") return { icon: FileLock, color: "var(--ic-muted)" };
  if (n.startsWith(".env") || ext === "pem" || ext === "key") return { icon: KeyRound, color: "var(--ic-js)" };
  if (n.startsWith(".git") || n === ".editorconfig" || n.startsWith(".golangci") || n.startsWith(".eslint") || n.startsWith(".prettier")) return { icon: Shield, color: "var(--ic-muted)" };
  if (ext === "md" || ext === "mdx" || n === "license") return { icon: BookOpen, color: "var(--ic-md)" };
  if (CODE[ext]) return { icon: FileCode, color: CODE[ext] };
  if (ext === "json" || ext === "jsonc") return { icon: Braces, color: "var(--ic-js)" };
  if (ext === "yml" || ext === "yaml" || ext === "toml" || ext === "ini" || ext === "conf" || ext === "cfg" || ext === "xml" || ext === "plist") return { icon: FileCog, color: "var(--ic-cfg)" };
  if (ext === "sh" || ext === "bash" || ext === "zsh" || ext === "fish") return { icon: Terminal, color: "var(--ic-go)" };
  if (ext === "sql") return { icon: Database, color: "var(--ic-kt)" };
  if (ext === "csv" || ext === "tsv" || ext === "xlsx") return { icon: FileSpreadsheet, color: "var(--ic-go)" };
  if (["png", "jpg", "jpeg", "gif", "svg", "webp", "ico", "icns"].includes(ext)) return { icon: Image, color: "var(--ic-kt)" };
  if (["ttf", "otf", "woff", "woff2"].includes(ext)) return { icon: FileType, color: "var(--ic-muted)" };
  if (ext === "txt" || ext === "log") return { icon: FileText, color: "var(--ic-muted)" };
  return { icon: File, color: "var(--ic-muted)" };
}

export function FileIcon(props: { name: string }) {
  const k = fileKind(props.name);
  const I = k.icon;
  return <I className="ficon" size={15} strokeWidth={1.8} color={k.color} aria-hidden="true" />;
}

export function FolderIcon(props: { open: boolean }) {
  const I = props.open ? FolderOpen : Folder;
  return <I className="ficon" size={15} strokeWidth={1.8} color="var(--ic-folder)" aria-hidden="true" />;
}
