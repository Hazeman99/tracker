import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import type { Project } from "../api";
import { boardLog, useData } from "../data";
import { formatDate, modKey, sameTag, taskCounts } from "../format";
import { PencilIcon, PlusIcon } from "./Icons";
import { InlineError, useInlineError } from "./InlineError";
import { SyncIndicator } from "./Sync";
import { TaskRow } from "./TaskRow";
import { useLingeringDone } from "../lingering";

type Props = {
  onOpen: (id: string) => void;
  onArchived: () => void;
};

export function Board({ onOpen, onArchived }: Props) {
  const { status, projects, setProjects, loaded, tagFilter, setTagFilter, readOnly } = useData();
  const [creating, setCreating] = useState(false);
  const [dragId, setDragId] = useState<string | null>(null);
  const [dropIndex, setDropIndex] = useState<number | null>(null);
  const [orderErr, showOrderErr] = useInlineError();
  const boardRef = useRef<HTMLElement>(null);

  const active = useMemo(() => projects.filter((p) => !p.archived_at), [projects]);

  // One chip per tag in use on the board, sorted by name.
  const tagsInUse = useMemo(() => {
    const out: string[] = [];
    for (const p of active)
      for (const t of p.tags ?? []) if (!out.some((x) => sameTag(x, t))) out.push(t);
    return out.sort((a, b) => a.localeCompare(b, undefined, { sensitivity: "base" }));
  }, [active]);

  // Remembered selections that no longer exist are ignored.
  const selected = useMemo(
    () => tagFilter.filter((t) => tagsInUse.some((x) => sameTag(x, t))),
    [tagFilter, tagsInUse],
  );
  const isSelected = (t: string) => selected.some((x) => sameTag(x, t));
  const toggleTag = (t: string) =>
    setTagFilter(isSelected(t) ? selected.filter((x) => !sameTag(x, t)) : [...selected, t]);
  const selectTag = (t: string) => {
    if (!isSelected(t)) setTagFilter([...selected, t]);
  };

  const visible = useMemo(
    () =>
      selected.length === 0
        ? active
        : active.filter((p) => (p.tags ?? []).some((t) => selected.some((s) => sameTag(s, t)))),
    [active, selected],
  );

  const startCreate = useCallback(() => {
    if (readOnly) return;
    setCreating(true);
    window.requestAnimationFrame(() => {
      const b = boardRef.current;
      if (b) b.scrollTo({ left: b.scrollWidth, behavior: "smooth" });
    });
  }, [readOnly]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (modKey(e) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === "n") {
        e.preventDefault();
        startCreate();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [startCreate]);

  const reorder = async (fromId: string, toIndex: number) => {
    const ids = visible.map((p) => p.id);
    const from = ids.indexOf(fromId);
    if (from < 0 || toIndex === from || toIndex === from + 1) return;
    ids.splice(from, 1);
    ids.splice(toIndex > from ? toIndex - 1 : toIndex, 0, fromId);
    // Full order: the visible columns in their new order, then everything
    // else (filtered-out and archived projects) in its existing order.
    const rest = projects.filter((p) => !ids.includes(p.id)).map((p) => p.id);
    const full = [...ids, ...rest];
    const prev = projects;
    const byId = new Map(projects.map((p) => [p.id, p]));
    setProjects(() => full.map((id, i) => ({ ...byId.get(id)!, position: i })));
    try {
      await api.reorderProjects(full);
    } catch (err) {
      setProjects(() => prev);
      showOrderErr(err);
    }
  };

  return (
    <div className="app">
      <header className="topbar">
        <div className="topbar-group">
          <span className="app-name">{status.app_name}</span>
          <SyncIndicator align="left" />
        </div>
        <div className="topbar-group" style={{ gap: 12 }}>
          <button type="button" className="btn-text" onClick={onArchived}>
            Archived
          </button>
          <button type="button" className="btn-primary" onClick={startCreate} disabled={readOnly}>
            <PlusIcon />
            <span>New project</span>
          </button>
        </div>
      </header>

      {tagsInUse.length > 0 && (
        <div className="filter-bar" role="group" aria-label="Filter by tag">
          {tagsInUse.map((t) => (
            <button
              key={t}
              type="button"
              className="chip filter-chip"
              aria-pressed={isSelected(t)}
              onClick={() => toggleTag(t)}
            >
              {t}
            </button>
          ))}
          {selected.length > 0 && (
            <button type="button" className="btn-text" onClick={() => setTagFilter([])}>
              Clear
            </button>
          )}
        </div>
      )}

      {orderErr && (
        <div style={{ padding: "8px 24px 0" }}>
          <InlineError message={orderErr} />
        </div>
      )}

      {loaded && selected.length > 0 && visible.length === 0 ? (
        <main className="board-empty">
          <p className="empty-note">No projects with these tags.</p>
        </main>
      ) : (
        <main className="board" aria-label="Projects" ref={boardRef}>
          {visible.map((p, i) => (
            <Column
              key={p.id}
              project={p}
              onOpen={() => onOpen(p.id)}
              onTag={selectTag}
              dragging={dragId === p.id}
              dropBefore={dropIndex === i && dragId !== null}
              onDragStart={() => setDragId(p.id)}
              onDragEnd={() => {
                setDragId(null);
                setDropIndex(null);
              }}
              onDragOverColumn={(before) => setDropIndex(before ? i : i + 1)}
              onDrop={() => {
                if (dragId !== null && dropIndex !== null) void reorder(dragId, dropIndex);
                setDragId(null);
                setDropIndex(null);
              }}
            />
          ))}
          <GhostColumn
              creating={creating}
              dropTarget={dragId !== null && dropIndex === visible.length}
              onStart={startCreate}
              onDone={() => setCreating(false)}
              onDragOver={() => setDropIndex(visible.length)}
              onDrop={() => {
                if (dragId !== null) void reorder(dragId, visible.length);
                setDragId(null);
                setDropIndex(null);
              }}
            />
        </main>
      )}
    </div>
  );
}

// ---------------------------------------------------------------- column

type ColumnProps = {
  project: Project;
  onOpen: () => void;
  onTag: (tag: string) => void;
  dragging: boolean;
  dropBefore: boolean;
  onDragStart: () => void;
  onDragEnd: () => void;
  onDragOverColumn: (before: boolean) => void;
  onDrop: () => void;
};

function Column({ project: p, onOpen, onTag, dragging, dropBefore, onDragStart, onDragEnd, onDragOverColumn, onDrop }: ColumnProps) {
  const { updateProject, readOnly } = useData();
  const [taskDraft, setTaskDraft] = useState("");
  const [composing, setComposing] = useState(false);
  const [logDraft, setLogDraft] = useState("");
  const [err, showErr] = useInlineError();
  const { lingering, start: startLinger, stop: stopLinger } = useLingeringDone();
  const tasks = p.tasks ?? [];
  // The board shows open tasks, plus tasks checked in the last few seconds.
  const shownTasks = tasks.filter((t) => !t.done || lingering.has(t.id));
  const log = boardLog(p.log ?? []);

  const toggle = async (id: string, done: boolean) => {
    if (done) startLinger(id);
    else stopLinger(id);
    updateProject(p.id, (x) => ({ ...x, tasks: x.tasks!.map((t) => (t.id === id ? { ...t, done } : t)) }));
    try {
      const t = await api.updateTask(id, { done });
      updateProject(p.id, (x) => ({ ...x, tasks: x.tasks!.map((y) => (y.id === id ? t : y)) }));
    } catch (e) {
      stopLinger(id);
      updateProject(p.id, (x) => ({ ...x, tasks: x.tasks!.map((t) => (t.id === id ? { ...t, done: !done } : t)) }));
      showErr(e);
    }
  };

  const addTask = async () => {
    const title = taskDraft.trim();
    if (!title) return;
    const tempId = `tmp-${crypto.randomUUID()}`;
    const now = new Date().toISOString();
    setTaskDraft("");
    updateProject(p.id, (x) => ({
      ...x,
      tasks: [...x.tasks!, { id: tempId, project_id: p.id, title, done: false, position: 1e9, completed_at: null, created_at: now, updated_at: now }],
    }));
    try {
      const t = await api.addTask(p.id, title);
      updateProject(p.id, (x) => ({ ...x, tasks: x.tasks!.map((y) => (y.id === tempId ? t : y)) }));
    } catch (e) {
      updateProject(p.id, (x) => ({ ...x, tasks: x.tasks!.filter((y) => y.id !== tempId) }));
      setTaskDraft(title);
      showErr(e);
    }
  };

  const addEntry = async () => {
    const body = logDraft.trim();
    if (!body) return;
    const tempId = `tmp-${crypto.randomUUID()}`;
    const now = new Date().toISOString();
    setComposing(false);
    setLogDraft("");
    updateProject(p.id, (x) => ({
      ...x,
      log: [{ id: tempId, project_id: p.id, body, created_at: now, updated_at: now }, ...x.log!],
    }));
    try {
      const e = await api.addLogEntry(p.id, body);
      updateProject(p.id, (x) => ({ ...x, log: x.log!.map((y) => (y.id === tempId ? e : y)) }));
    } catch (e) {
      updateProject(p.id, (x) => ({ ...x, log: x.log!.filter((y) => y.id !== tempId) }));
      setLogDraft(body);
      setComposing(true);
      showErr(e);
    }
  };

  return (
    <section
      className="column"
      aria-label={p.name}
      data-dragging={dragging}
      data-drop={dropBefore ? "before" : undefined}
      onDragOver={(e) => {
        if (!e.dataTransfer.types.includes("application/x-tracker-project")) return;
        e.preventDefault();
        const r = e.currentTarget.getBoundingClientRect();
        onDragOverColumn(e.clientX < r.left + r.width / 2);
      }}
      onDrop={(e) => {
        e.preventDefault();
        onDrop();
      }}
    >
      <div className="column-scroll">
        <div
          className="column-header"
          draggable={!readOnly}
          onDragStart={(e) => {
            e.dataTransfer.setData("application/x-tracker-project", p.id);
            e.dataTransfer.effectAllowed = "move";
            onDragStart();
          }}
          onDragEnd={onDragEnd}
          title="Drag to reorder"
        >
          <h2 className="column-title">
            <button type="button" className="title-link" onClick={onOpen}>
              {p.name}
            </button>
          </h2>
          <span className="count">{taskCounts(tasks)}</span>
        </div>

        {(p.tags ?? []).length > 0 && (
          <div className="chip-row" aria-label="Tags">
            {p.tags!.map((t) => (
              <button key={t} type="button" className="chip" onClick={() => onTag(t)} title={`Filter by ${t}`}>
                {t}
              </button>
            ))}
          </div>
        )}

        {p.description && <p className="column-desc">{p.description}</p>}

        <div className="column-section">
          <h3 className="section-label">Tasks</h3>
          {shownTasks.map((t) => (
            <div key={t.id} className="task-linger" data-leaving={t.done ? "true" : undefined}>
              <TaskRow task={t} readOnly={readOnly || t.id.startsWith("tmp-")} onToggle={() => void toggle(t.id, !t.done)} />
            </div>
          ))}
          {tasks.length > 0 && shownTasks.length === 0 && <p className="empty-note">All tasks done.</p>}
          <input
            type="text"
            className="field"
            style={{ marginTop: 4 }}
            aria-label={`Add a task to ${p.name}`}
            placeholder="Add a task…"
            maxLength={500}
            disabled={readOnly}
            value={taskDraft}
            onChange={(e) => setTaskDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                void addTask();
              }
            }}
          />
        </div>

        <div className="column-section column-log">
          <h3 className="section-label">Log</h3>
          {log.length === 0 && <p className="empty-note">No entries yet.</p>}
          {log.map((e) => (
            <div key={e.id} className="board-entry">
              <span className="entry-date">{formatDate(e.created_at)}</span>
              <p>{e.body}</p>
            </div>
          ))}
        </div>
        <InlineError message={err} />
      </div>

      <div className="column-footer">
        {composing ? (
          <div className="composer">
            <textarea
              className="field"
              aria-label={`New log entry for ${p.name}`}
              placeholder="What happened?"
              autoFocus
              value={logDraft}
              disabled={readOnly}
              onChange={(e) => setLogDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && modKey(e)) {
                  e.preventDefault();
                  void addEntry();
                } else if (e.key === "Escape") {
                  e.preventDefault();
                  e.stopPropagation();
                  setComposing(false);
                }
              }}
            />
            <div className="row-end">
              <button type="button" className="btn-secondary" onClick={() => setComposing(false)}>
                Cancel
              </button>
              <button type="button" className="btn-primary" disabled={readOnly || !logDraft.trim()} onClick={() => void addEntry()}>
                Save
              </button>
            </div>
          </div>
        ) : (
          <button type="button" className="btn-secondary" disabled={readOnly} onClick={() => setComposing(true)}>
            <PencilIcon />
            <span>Write log entry</span>
          </button>
        )}
      </div>
    </section>
  );
}

// ---------------------------------------------------------------- ghost column

type GhostProps = {
  creating: boolean;
  dropTarget: boolean;
  onStart: () => void;
  onDone: () => void;
  onDragOver: () => void;
  onDrop: () => void;
};

function GhostColumn({ creating, dropTarget, onStart, onDone, onDragOver, onDrop }: GhostProps) {
  const { setProjects, readOnly } = useData();
  const [name, setName] = useState("");
  const [err, showErr] = useInlineError();
  const dnd = {
    onDragOver: (e: React.DragEvent) => {
      if (!e.dataTransfer.types.includes("application/x-tracker-project")) return;
      e.preventDefault();
      onDragOver();
    },
    onDrop: (e: React.DragEvent) => {
      e.preventDefault();
      onDrop();
    },
  };
  const style = dropTarget ? { outline: "2px solid var(--accent)", outlineOffset: 4 } : undefined;

  const create = async () => {
    const n = name.trim();
    if (!n) return;
    const tempId = `tmp-${crypto.randomUUID()}`;
    const now = new Date().toISOString();
    setName("");
    onDone();
    setProjects((prev) => [
      ...prev,
      { id: tempId, name: n, description: "", position: 1e9, archived_at: null, created_at: now, updated_at: now, tags: [], tasks: [], log: [] },
    ]);
    try {
      const p = await api.createProject(n);
      setProjects((prev) => prev.map((x) => (x.id === tempId ? p : x)));
    } catch (e) {
      setProjects((prev) => prev.filter((x) => x.id !== tempId));
      setName(n);
      onStart();
      showErr(e);
    }
  };

  if (!creating) {
    return (
      <div className="stack" style={{ flex: "0 0 340px", maxWidth: "86vw", gap: 8 }}>
        <button type="button" className="ghost-column" style={{ flex: 1, ...style }} onClick={onStart} disabled={readOnly} {...dnd}>
          <PlusIcon size={18} />
          <span>New project</span>
        </button>
        <InlineError message={err} />
      </div>
    );
  }
  return (
    <div className="ghost-form" style={style} {...dnd}>
      <label className="section-label" htmlFor="new-project-name">
        New project
      </label>
      <input
        id="new-project-name"
        className="field"
        placeholder="Project name"
        autoFocus
        maxLength={200}
        value={name}
        onChange={(e) => setName(e.target.value)}
        onBlur={() => {
          if (!name.trim()) onDone();
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            void create();
          } else if (e.key === "Escape") {
            e.stopPropagation();
            setName("");
            onDone();
          }
        }}
      />
      <p className="empty-note">Press Enter to create, Esc to cancel.</p>
      <InlineError message={err} />
    </div>
  );
}
