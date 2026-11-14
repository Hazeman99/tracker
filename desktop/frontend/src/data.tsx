import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { api, setConnectionListeners } from "./api";
import type { Project, Status } from "./api";

export type SyncState = "synced" | "syncing" | "offline";

type Data = {
  status: Status;
  projects: Project[];
  loaded: boolean;
  setProjects: (fn: (prev: Project[]) => Project[]) => void;
  updateProject: (id: string, fn: (p: Project) => Project) => void;
  refresh: () => Promise<void>;
  sync: SyncState;
  readOnly: boolean;
  /** Increments on window focus and every 60 s; pages refetch when it changes. */
  tick: number;
  tagFilter: string[];
  setTagFilter: (tags: string[]) => void;
  signOut: () => Promise<void>;
};

const Ctx = createContext<Data | null>(null);

export function useData(): Data {
  const v = useContext(Ctx);
  if (!v) throw new Error("useData outside DataProvider");
  return v;
}

const REFRESH_MS = 60_000;

export function DataProvider({
  status,
  onSignedOut,
  children,
}: {
  status: Status;
  onSignedOut: () => void;
  children: ReactNode;
}) {
  const [projects, setProjectsState] = useState<Project[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [sync, setSync] = useState<SyncState>("syncing");
  const [tick, setTick] = useState(0);
  const [tagFilter, setTagFilterState] = useState<string[]>([]);
  const inflight = useRef(false);

  useEffect(() => {
    setConnectionListeners({
      unauthorized: onSignedOut,
      offline: () => setSync("offline"),
      online: () => setSync((s) => (s === "offline" ? "synced" : s)),
    });
  }, [onSignedOut]);

  const refresh = useCallback(async () => {
    if (inflight.current) return;
    inflight.current = true;
    setSync((s) => (s === "offline" ? s : "syncing"));
    try {
      const list = await api.listProjects();
      setProjectsState(list);
      setLoaded(true);
      setSync("synced");
    } catch {
      // offline/unauthorized are reported through the listeners; keep the
      // last loaded data visible.
      setSync((s) => (s === "syncing" ? "offline" : s));
    } finally {
      inflight.current = false;
    }
  }, []);

  useEffect(() => {
    void refresh();
    void api.tagFilter().then(setTagFilterState);
  }, [refresh]);

  useEffect(() => {
    const bump = () => {
      setTick((t) => t + 1);
      void refresh();
    };
    const onVisible = () => {
      if (document.visibilityState === "visible") bump();
    };
    window.addEventListener("focus", bump);
    document.addEventListener("visibilitychange", onVisible);
    const id = window.setInterval(bump, REFRESH_MS);
    return () => {
      window.removeEventListener("focus", bump);
      document.removeEventListener("visibilitychange", onVisible);
      window.clearInterval(id);
    };
  }, [refresh]);

  const setProjects = useCallback((fn: (prev: Project[]) => Project[]) => setProjectsState(fn), []);
  const updateProject = useCallback(
    (id: string, fn: (p: Project) => Project) =>
      setProjectsState((prev) => prev.map((p) => (p.id === id ? fn(p) : p))),
    [],
  );
  const setTagFilter = useCallback((tags: string[]) => {
    setTagFilterState(tags);
    void api.setTagFilter(tags);
  }, []);
  const signOut = useCallback(async () => {
    await api.signOut();
    onSignedOut();
  }, [onSignedOut]);

  const value = useMemo<Data>(
    () => ({
      status,
      projects,
      loaded,
      setProjects,
      updateProject,
      refresh,
      sync,
      readOnly: sync === "offline",
      tick,
      tagFilter,
      setTagFilter,
      signOut,
    }),
    [status, projects, loaded, setProjects, updateProject, refresh, sync, tick, tagFilter, setTagFilter, signOut],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

/** Board view of a project: the 2 most recent log entries. */
export function boardLog<T extends { created_at: string }>(log: T[]): T[] {
  return [...log].sort((a, b) => b.created_at.localeCompare(a.created_at)).slice(0, 2);
}
