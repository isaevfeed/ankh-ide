import { Call, Events } from "@wailsio/runtime";
import type { SearchOpts } from "./search";
import type { ACPInfo, SearchResult, Comment, DiffMode, DiffResult, Entry, FileContent, MRInfo, Project, Session, Settings, StateDTO, Usage } from "./types";

const SVC = "ankh-ide/internal/app.Service.";

function call<T>(method: string, ...args: unknown[]): Promise<T> {
  return Call.ByName(SVC + method, ...args) as Promise<T>;
}

export const api = {
  getState: () => call<StateDTO>("GetState"),
  updateSettings: (s: Settings) => call<void>("UpdateSettings", s),

  addProject: () => call<Project | null>("AddProject"),
  addProjectPath: (path: string) => call<Project>("AddProjectPath", path),
  removeProject: (id: string) => call<void>("RemoveProject", id),
  setActive: (projectId: string, sessionId: string) => call<void>("SetActive", projectId, sessionId),
  markSeen: (sessionId: string) => call<void>("MarkSeen", sessionId),
  branches: (projectId: string) => call<string[] | null>("Branches", projectId),
  createSession: (projectId: string, branch: string, base: string, agent: string, setup: boolean) =>
    call<Session>("CreateSession", projectId, branch, base, agent, setup),
  closeSession: (sessionId: string, force: boolean, deleteBranch: boolean) =>
    call<void>("CloseSession", sessionId, force, deleteBranch),
  updateProject: (projectId: string, setupScript: string) => call<void>("UpdateProject", projectId, setupScript),
  setSessionAgent: (sessionId: string, agent: string) => call<void>("SetSessionAgent", sessionId, agent),

  startAgent: (sessionId: string, cols: number, rows: number) => call<string>("StartAgent", sessionId, cols, rows),
  restartAgent: (sessionId: string, cols: number, rows: number, fresh: boolean) =>
    call<string>("RestartAgent", sessionId, cols, rows, fresh),
  startShell: (sessionId: string, cols: number, rows: number) => call<string>("StartShell", sessionId, cols, rows),
  termWrite: (id: string, data: string) => call<void>("TermWrite", id, data),
  termResize: (id: string, cols: number, rows: number) => call<void>("TermResize", id, cols, rows),
  termKill: (id: string) => call<void>("TermKill", id),
  termRunning: (id: string) => call<boolean>("TermRunning", id),
  sendPrompt: (sessionId: string, text: string, submit: boolean) => call<void>("SendPrompt", sessionId, text, submit),

  listDir: (sessionId: string, rel: string) => call<Entry[] | null>("ListDir", sessionId, rel),
  readFile: (sessionId: string, rel: string) => call<FileContent>("ReadFile", sessionId, rel),
  gitStatus: (sessionId: string) => call<Record<string, string> | null>("GitStatus", sessionId),
  getDiff: (sessionId: string, mode: DiffMode) => call<DiffResult>("GetDiff", sessionId, mode),
  reveal: (sessionId: string, rel: string) => call<void>("Reveal", sessionId, rel),
  openExternal: (sessionId: string, rel: string, app: string) => call<void>("OpenExternal", sessionId, rel, app),
  copy: (text: string) => call<boolean>("Copy", text),
  notify: (title: string, body: string) => call<void>("Notify", title, body),

  addComment: (sessionId: string, c: Partial<Comment>) => call<Comment>("AddComment", sessionId, c),
  updateComment: (sessionId: string, id: string, text: string) => call<void>("UpdateComment", sessionId, id, text),
  deleteComment: (sessionId: string, id: string) => call<void>("DeleteComment", sessionId, id),
  discardDrafts: (sessionId: string, all: boolean) => call<void>("DiscardDrafts", sessionId, all),
  submitReview: (sessionId: string, chat: boolean) => call<string>("SubmitReview", sessionId, chat),
  sendCommand: (sessionId: string, cmd: string) => call<void>("SendCommand", sessionId, cmd),
  requestUsage: (sessionId: string) => call<string>("RequestUsage", sessionId),
  usage: () => call<Usage | null>("Usage"),

  search: (sessionId: string, query: string, opts: SearchOpts) => call<SearchResult>("Search", sessionId, query, opts),
  listFiles: (sessionId: string) => call<string[] | null>("ListFiles", sessionId),
  stage: (sessionId: string, paths: string[]) => call<void>("Stage", sessionId, paths),
  unstage: (sessionId: string, paths: string[]) => call<void>("Unstage", sessionId, paths),
  discard: (sessionId: string, paths: string[]) => call<void>("Discard", sessionId, paths),
  commit: (sessionId: string, message: string, all: boolean) => call<string>("Commit", sessionId, message, all),

  mrStatus: (sessionId: string) => call<MRInfo>("MRStatus", sessionId),
  mrCreate: (sessionId: string, draft: boolean) => call<MRInfo>("MRCreate", sessionId, draft),
  mrPostComments: (sessionId: string) => call<string>("MRPostComments", sessionId),
  mrImport: (sessionId: string) => call<number>("MRImport", sessionId),
  openURL: (url: string) => call<void>("OpenURL", url),

  acpState: (sessionId: string) => call<ACPInfo>("ACPState", sessionId),
  acpStart: (sessionId: string) => call<void>("ACPStart", sessionId),
  acpPrompt: (sessionId: string, text: string) => call<void>("ACPPrompt", sessionId, text),
  acpCancel: (sessionId: string) => call<void>("ACPCancel", sessionId),
  acpPermission: (sessionId: string, permId: string, optionId: string) => call<void>("ACPPermission", sessionId, permId, optionId),
  acpStop: (sessionId: string) => call<void>("ACPStop", sessionId),
  acpClear: (sessionId: string) => call<void>("ACPClear", sessionId),
};

export function on<T>(name: string, cb: (data: T) => void): () => void {
  return Events.On(name, (e: { data: unknown }) => cb(e.data as T));
}

export function errText(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (typeof e === "string") return e;
  try {
    const o = e as { message?: string };
    if (o && typeof o.message === "string") return o.message;
    return JSON.stringify(e);
  } catch {
    return String(e);
  }
}
