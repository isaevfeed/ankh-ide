export type Status = "idle" | "working" | "waiting";
export type DiffMode = "wip" | "branch" | "last";

export interface Settings {
  autoSend: boolean;
  defaultAgent: string;
  showIgnored: boolean;
  diffMode: DiffMode;
  editor: string;
  tmux: boolean;
  acp?: Record<string, string> | null;
}

export interface Comment {
  id: string;
  path: string;
  side: "new" | "old";
  line: number;
  lineEnd?: number;
  col?: number;
  loc: string;
  anchor: string;
  before?: string;
  deleted?: boolean;
  text: string;
  sent?: boolean;
  outdated?: boolean;
  movedFrom?: number;
  target?: "" | "mr";
  mrNote?: number;
  author?: string;
}

export interface Session {
  id: string;
  branch: string;
  path: string;
  worktree: boolean;
  base?: string;
  agent: string;
  started: boolean;
  comments: Comment[] | null;
}

export interface Project {
  id: string;
  name: string;
  path: string;
  setupScript: string;
  assistant?: boolean;
  sessions: Session[];
  activeSession: string;
}

export interface Preset {
  id: string;
  name: string;
  command: string;
  claude: boolean;
  fileRef: string;
  acp: string;
}

export interface UsageWindow {
  usedPercentage: number;
  resetsAt: number;
  resetsText?: string;
}

export interface Usage {
  fiveHour?: UsageWindow;
  sevenDay?: UsageWindow;
  model?: string;
  at: number;
  source: string;
}

export interface StateDTO {
  data: { projects: Project[] | null; activeProject: string; settings: Settings };
  status: Record<string, Status>;
  usage: Usage | null;
  presets: Preset[];
  home: string;
  tmux: string;
  glab: string;
}

export interface Entry {
  name: string;
  path: string;
  dir: boolean;
  ignored: boolean;
}

export interface FileContent {
  path: string;
  content: string;
  binary: boolean;
  tooBig: boolean;
  size: number;
}

export interface DiffLine {
  t: " " | "+" | "-";
  o: number;
  n: number;
  text: string;
}

export interface Hunk {
  header: string;
  lines: DiffLine[];
}

export interface FileDiff {
  path: string;
  oldPath?: string;
  status: "M" | "A" | "D" | "R";
  add: number;
  del: number;
  binary: boolean;
  hunks: Hunk[] | null;
}

export interface DiffResult {
  mode: DiffMode;
  base: string;
  branch: string;
  files: FileDiff[] | null;
  index?: Record<string, "" | "staged" | "partial"> | null;
  ahead: number;
  upstream: string;
  relocated?: number;
}

export interface BranchList {
  current: string;
  local: string[] | null;
  remote: string[] | null;
}

export type View =
  | { type: "agent" }
  | { type: "shell"; termId: string; n: number }
  | { type: "file"; path: string }
  | { type: "chat" };

export interface AgentContext {
  usedPercentage: number;
  tokens: number;
  window: number;
}

export interface SearchHit {
  path: string;
  line: number;
  text: string;
  ranges: [number, number][] | null;
}

export interface SearchResult {
  hits: SearchHit[] | null;
  files: number;
  truncated: boolean;
}

export interface MR {
  iid: number;
  project_id: number;
  title: string;
  web_url: string;
  state: string;
  draft: boolean;
  source_branch: string;
  target_branch: string;
  detailed_merge_status: string;
  user_notes_count: number;
  head_pipeline?: { status: string; web_url: string } | null;
  unresolved: number;
}

export interface MRInfo {
  mr: MR | null;
  pushed: boolean;
  branch: string;
  base: string;
}

export type ACPState = "" | "starting" | "ready" | "working" | "closed";

export interface ACPPermission {
  id: string;
  toolCall?: { toolCallId?: string; title?: string; kind?: string } | null;
  options?: { optionId: string; name: string; kind: string }[] | null;
  chosen?: string;
}

export interface ACPEvent {
  kind: "user" | "update" | "permission" | "permission:done" | "error" | "stop";
  text?: string;
  update?: Record<string, unknown> & { sessionUpdate: string };
  perm?: ACPPermission;
}

export interface ACPInfo {
  state: ACPState;
  error?: string;
  command: string;
  events: ACPEvent[];
}
