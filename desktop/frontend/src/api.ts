// Thin wrapper over the generated Wails bindings. Every network call happens
// in Go; this file only classifies the errors Go returns.
import * as Svc from "../bindings/tracker/desktop/internal/tracker/service";
import type {
  LogEntry,
  Project,
  ProjectPatch,
  Session,
  Task,
  TaskPatch,
} from "../bindings/tracker/desktop/internal/api/models";
import type { Status } from "../bindings/tracker/desktop/internal/tracker/models";

export type { LogEntry, Project, ProjectPatch, Session, Task, TaskPatch, Status };

export type ErrorKind = "unauthorized" | "offline" | "invalid" | "rate_limited" | "server";

export class AppError extends Error {
  kind: ErrorKind;
  constructor(kind: ErrorKind, message: string) {
    super(message);
    this.kind = kind;
  }
}

type Listeners = {
  unauthorized: () => void;
  offline: () => void;
  online: () => void;
};
const listeners: Listeners = { unauthorized: () => {}, offline: () => {}, online: () => {} };

export function setConnectionListeners(l: Partial<Listeners>) {
  Object.assign(listeners, l);
}

function toAppError(err: unknown): AppError {
  if (err instanceof AppError) return err;
  const e = err as { cause?: unknown; message?: string };
  let cause = e?.cause;
  if (typeof cause === "string") {
    try {
      cause = JSON.parse(cause);
    } catch {
      cause = undefined;
    }
  }
  const c = cause as { kind?: string; message?: string } | undefined;
  if (c && typeof c.kind === "string") {
    return new AppError(c.kind as ErrorKind, c.message || "Something went wrong");
  }
  return new AppError("server", e?.message || "Something went wrong");
}

/** Runs a bound call, reporting connectivity and sign-out to the app. */
async function run<T>(p: Promise<T>): Promise<T> {
  try {
    const v = await p;
    listeners.online();
    return v;
  } catch (err) {
    const e = toAppError(err);
    if (e.kind === "unauthorized") listeners.unauthorized();
    else if (e.kind === "offline") listeners.offline();
    else listeners.online();
    throw e;
  }
}

function normalizeProject(p: Project): Project {
  return { ...p, tags: p.tags ?? [], tasks: p.tasks ?? [], log: p.log ?? [] };
}

export const api = {
  status: (): Promise<Status> => Svc.Status(),
  connect: async (server: string, email: string, password: string, insecureTLS: boolean) => {
    try {
      await Svc.Connect(server, email, password, insecureTLS);
    } catch (err) {
      throw toAppError(err);
    }
  },
  signOut: () => Svc.SignOut().catch(() => undefined),

  listProjects: async () => ((await run(Svc.ListProjects())) ?? []).map(normalizeProject),
  getProject: async (id: string) => normalizeProject(await run(Svc.GetProject(id))),
  createProject: async (name: string) => normalizeProject(await run(Svc.CreateProject(name))),
  updateProject: async (id: string, patch: ProjectPatch) => normalizeProject(await run(Svc.UpdateProject(id, patch))),
  deleteProject: (id: string) => run(Svc.DeleteProject(id)),
  reorderProjects: (ids: string[]) => run(Svc.ReorderProjects(ids)),

  addLogEntry: (projectID: string, body: string) => run(Svc.AddLogEntry(projectID, body)),
  updateLogEntry: (id: string, body: string) => run(Svc.UpdateLogEntry(id, body)),
  deleteLogEntry: (id: string) => run(Svc.DeleteLogEntry(id)),

  addTask: (projectID: string, title: string) => run(Svc.AddTask(projectID, title)),
  updateTask: (id: string, patch: TaskPatch) => run(Svc.UpdateTask(id, patch)),
  deleteTask: (id: string) => run(Svc.DeleteTask(id)),

  listSessions: async () => (await run(Svc.ListSessions())) ?? [],
  revokeSession: (id: string, current: boolean) => run(Svc.RevokeSession(id, current)),
  changePassword: (current: string, next: string) => run(Svc.ChangePassword(current, next)),

  completeOnboarding: () => Svc.CompleteOnboarding().catch(() => undefined),

  tagFilter: async () => (await Svc.TagFilter()) ?? [],
  setTagFilter: (tags: string[]) => Svc.SetTagFilter(tags).catch(() => undefined),
};

export function errorMessage(err: unknown): string {
  const e = toAppError(err);
  if (e.kind === "offline") return "Offline. The change was not saved.";
  const m = e.message;
  return m.charAt(0).toUpperCase() + m.slice(1);
}
