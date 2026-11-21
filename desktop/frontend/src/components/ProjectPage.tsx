import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, AppError } from "../api";
import type { LogEntry, Project } from "../api";
import { useData } from "../data";
import { formatDate, modKey, sameTag, taskCounts } from "../format";
import { useConfirm } from "./Confirm";
import { BackIcon, ChevronIcon, CloseIcon, PlusIcon } from "./Icons";
import { InlineError, useInlineError } from "./InlineError";
import { Menu } from "./Menu";
import { SyncIndicator } from "./Sync";
import { TaskRow } from "./TaskRow";
import { useLingeringDone } from "../lingering";

const MAX_TAGS = 20;

export function ProjectPage({ id, onBack }: { id: string; onBack: () => void }) {
  const { projects, updateProject, setProjects, readOnly, tick, refresh } = useData();
  const fromBoard = projects.find((p) => p.id === id) ?? null;
  const [project, setProject] = useState<Project | null>(fromBoard);
  const [err, showErr] = useInlineError();
  const confirm = useConfirm();
  const { lingering, start: startLinger, stop: stopLinger } = useLingeringDone();
  const [showDone, setShowDone] = useState(false);

  // Load the full project (all tasks and entries); refetch on focus / every 60 s.
  useEffect(() => {
    let live = true;
    api.getProject(id).then(
      (p) => live && setProject(p),
      (e) => {
        if (live && e instanceof AppError && e.kind === "invalid" && (e.message === "not found" || e.message === "invalid id")) {
          void refresh();
          onBack();
        }
      },
    );
    return () => {
      live = false;
    };
  }, [id, tick, onBack, refresh]);

  // Esc goes back to the board (editors stop the event themselves).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      const t = e.target as HTMLElement;
      if (t.closest("input, textarea, dialog, [role=menu], .popover")) return;
      onBack();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onBack]);

  /** Applies a change to this page and to the board copy. */
  const apply = useCallback(
    (fn: (p: Project) => Project) => {
      setProject((p) => (p ? fn(p) : p));
      updateProject(id, fn);
    },
    [id, updateProject],
  );

  /** Optimistic update with rollback. */
  const mutate = useCallback(
    async <T,>(change: (p: Project) => Project, undo: (p: Project) => Project, req: () => Promise<T>, after?: (v: T) => void) => {
      apply(change);
      try {
        const v = await req();
        after?.(v);
      } catch (e) {
        apply(undo);
        showErr(e);
      }
    },
    [apply, showErr],
  );

  const allTags = useMemo(() => {
    const out: string[] = [];
    for (const p of projects) for (const t of p.tags ?? []) if (!out.some((x) => sameTag(x, t))) out.push(t);
    return out.sort((a, b) => a.localeCompare(b, undefined, { sensitivity: "base" }));
  }, [projects]);

  if (!project) {
    return (
      <div className="app">
        <PageBar onBack={onBack} />
        <div className="page-scroll">
          <div className="project-page">
            <p className="empty-note">Loading…</p>
          </div>
        </div>
      </div>
    );
  }
  const p = project;

  const setField = (field: "name" | "description", value: string) => {
    const old = p[field];
    if (value === old) return;
    void mutate(
      (x) => ({ ...x, [field]: value }),
      (x) => ({ ...x, [field]: old }),
      () => api.updateProject(p.id, { [field]: value }),
      (np) => apply((x) => ({ ...x, [field]: np[field], updated_at: np.updated_at })),
    );
  };

  const setTags = (tags: string[]) => {
    const old = p.tags ?? [];
    void mutate(
      (x) => ({ ...x, tags }),
      (x) => ({ ...x, tags: old }),
      () => api.updateProject(p.id, { tags }),
      (np) => {
        apply((x) => ({ ...x, tags: np.tags }));
      },
    );
  };

  const archive = async () => {
    try {
      await api.updateProject(p.id, { archived: true });
      updateProject(p.id, (x) => ({ ...x, archived_at: new Date().toISOString() }));
      onBack();
    } catch (e) {
      showErr(e);
    }
  };

  const remove = async () => {
    if (!(await confirm("Delete project?", `“${p.name}” and all of its tasks and log entries will be deleted. This cannot be undone.`))) return;
    try {
      await api.deleteProject(p.id);
      setProjects((prev) => prev.filter((x) => x.id !== p.id));
      onBack();
    } catch (e) {
      showErr(e);
    }
  };

  const tasks = p.tasks ?? [];
  // Open tasks (plus tasks checked in the last few seconds), then the done
  // tasks, newest first, behind the collapsed "Done" toggle.
  const openTasks = tasks.filter((t) => !t.done || lingering.has(t.id));
  const doneTasks = tasks
    .filter((t) => t.done && !lingering.has(t.id))
    .sort((a, b) => (b.completed_at ?? "").localeCompare(a.completed_at ?? ""));
  const log = [...(p.log ?? [])].sort((a, b) => b.created_at.localeCompare(a.created_at));

  const addTask = async (title: string) => {
    const tempId = `tmp-${crypto.randomUUID()}`;
    const now = new Date().toISOString();
    await mutate(
      (x) => ({ ...x, tasks: [...(x.tasks ?? []), { id: tempId, project_id: p.id, title, done: false, position: 1e9, completed_at: null, created_at: now, updated_at: now }] }),
      (x) => ({ ...x, tasks: (x.tasks ?? []).filter((t) => t.id !== tempId) }),
      () => api.addTask(p.id, title),
      (t) => apply((x) => ({ ...x, tasks: (x.tasks ?? []).map((y) => (y.id === tempId ? t : y)) })),
    );
  };

  const patchTask = (taskId: string, patch: { title?: string; done?: boolean }) => {
    const old = tasks.find((t) => t.id === taskId);
    if (!old) return;
    if (patch.done === true) startLinger(taskId);
    if (patch.done === false) stopLinger(taskId);
    void mutate(
      (x) => ({ ...x, tasks: (x.tasks ?? []).map((t) => (t.id === taskId ? { ...t, ...patch } : t)) }),
      (x) => ({ ...x, tasks: (x.tasks ?? []).map((t) => (t.id === taskId ? old : t)) }),
      () => api.updateTask(taskId, patch),
      (t) => apply((x) => ({ ...x, tasks: (x.tasks ?? []).map((y) => (y.id === taskId ? t : y)) })),
    );
  };

  const deleteTask = async (taskId: string) => {
    const old = tasks.find((t) => t.id === taskId);
    if (!old || !(await confirm("Delete task?", `“${old.title}” will be deleted.`))) return;
    void mutate(
      (x) => ({ ...x, tasks: (x.tasks ?? []).filter((t) => t.id !== taskId) }),
      (x) => ({ ...x, tasks: [...(x.tasks ?? []), old].sort((a, b) => a.position - b.position) }),
      () => api.deleteTask(taskId),
    );
  };

  const addEntry = async (body: string) => {
    const tempId = `tmp-${crypto.randomUUID()}`;
    const now = new Date().toISOString();
    await mutate(
      (x) => ({ ...x, log: [{ id: tempId, project_id: p.id, body, created_at: now, updated_at: now }, ...(x.log ?? [])] }),
      (x) => ({ ...x, log: (x.log ?? []).filter((e) => e.id !== tempId) }),
      () => api.addLogEntry(p.id, body),
      (e) => apply((x) => ({ ...x, log: (x.log ?? []).map((y) => (y.id === tempId ? e : y)) })),
    );
  };

  const editEntry = (entry: LogEntry, body: string) => {
    if (body === entry.body) return;
    void mutate(
      (x) => ({ ...x, log: (x.log ?? []).map((e) => (e.id === entry.id ? { ...e, body } : e)) }),
      (x) => ({ ...x, log: (x.log ?? []).map((e) => (e.id === entry.id ? entry : e)) }),
      () => api.updateLogEntry(entry.id, body),
    );
  };

  const deleteEntry = async (entry: LogEntry) => {
    if (!(await confirm("Delete log entry?", "This entry will be deleted."))) return;
    void mutate(
      (x) => ({ ...x, log: (x.log ?? []).filter((e) => e.id !== entry.id) }),
      (x) => ({ ...x, log: [...(x.log ?? []), entry] }),
      () => api.deleteLogEntry(entry.id),
    );
  };

  const taskRow = (t: (typeof tasks)[number]) => (
    <TaskRow
      key={t.id}
      task={t}
      large
      readOnly={readOnly || t.id.startsWith("tmp-")}
      onToggle={() => patchTask(t.id, { done: !t.done })}
      onRename={(title) => patchTask(t.id, { title })}
      onDelete={() => void deleteTask(t.id)}
    />
  );

  return (
    <div className="app">
      <PageBar onBack={onBack} />
      <div className="page-scroll">
        <div className="project-page">
          <main className="project-main">
            <div className="project-head">
              <div className="heading-row">
                <EditableTitle name={p.name} readOnly={readOnly} onSave={(v) => setField("name", v)} />
                <div className="heading-actions">
                  <DescriptionButton readOnly={readOnly} />
                  <Menu
                    label="Project actions"
                    disabled={readOnly}
                    items={[
                      { label: "Archive project", onSelect: () => void archive() },
                      { label: "Delete project…", onSelect: () => void remove() },
                    ]}
                  />
                </div>
              </div>
              <TagEditor tags={p.tags ?? []} all={allTags} readOnly={readOnly} onChange={setTags} />
              <InlineError message={err} />
              <Description text={p.description} readOnly={readOnly} onSave={(v) => setField("description", v)} />
            </div>

            <section className="log-section" aria-labelledby="log-heading">
              <h2 className="section-label" id="log-heading">
                Log
              </h2>
              <LogComposer readOnly={readOnly} onAdd={addEntry} />
              <div className="entries">
                {log.length === 0 && <p className="empty-note">No entries yet.</p>}
                {log.map((e) => (
                  <Entry
                    key={e.id}
                    entry={e}
                    readOnly={readOnly || e.id.startsWith("tmp-")}
                    onSave={(body) => editEntry(e, body)}
                    onDelete={() => void deleteEntry(e)}
                  />
                ))}
              </div>
            </section>
          </main>

          <aside className="tasks-card" aria-labelledby="tasks-heading">
            <div className="tasks-card-head">
              <h2 className="section-label" id="tasks-heading">
                Tasks
              </h2>
              <span className="count">{taskCounts(tasks)}</span>
            </div>
            <AddTaskForm readOnly={readOnly} onAdd={addTask} />
            <div className="task-list">
              {openTasks.map((t) => (
                <div key={t.id} className="task-linger" data-leaving={t.done ? "true" : undefined}>
                  {taskRow(t)}
                </div>
              ))}
              {openTasks.length === 0 && tasks.length > 0 && <p className="empty-note">All tasks done.</p>}
            </div>
            {doneTasks.length > 0 && (
              <div className="done-tasks">
                <button
                  type="button"
                  className="done-toggle"
                  aria-expanded={showDone}
                  aria-controls="done-task-list"
                  onClick={() => setShowDone((v) => !v)}
                >
                  <ChevronIcon open={showDone} />
                  <span>Done</span>
                  <span className="count">{doneTasks.length}</span>
                </button>
                {showDone && (
                  <div className="task-list" id="done-task-list">
                    {doneTasks.map(taskRow)}
                  </div>
                )}
              </div>
            )}
          </aside>
        </div>
      </div>
    </div>
  );
}

function PageBar({ onBack }: { onBack: () => void }) {
  return (
    <header className="topbar">
      <button type="button" className="back-link" onClick={onBack}>
        <BackIcon />
        <span>All projects</span>
      </button>
      <SyncIndicator align="right" />
    </header>
  );
}

// ---------------------------------------------------------------- name

function EditableTitle({ name, readOnly, onSave }: { name: string; readOnly: boolean; onSave: (v: string) => void }) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(name);
  if (editing) {
    const save = () => {
      const v = draft.trim();
      setEditing(false);
      if (v) onSave(v);
    };
    return (
      <input
        className="title-input"
        aria-label="Project name"
        value={draft}
        autoFocus
        maxLength={200}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={save}
        onKeyDown={(e) => {
          if (e.key === "Enter") save();
          if (e.key === "Escape") {
            e.stopPropagation();
            setEditing(false);
          }
        }}
      />
    );
  }
  return (
    <h1 className="page-title">
      <button
        type="button"
        className="title-link"
        title="Rename project"
        disabled={readOnly}
        onClick={() => {
          setDraft(name);
          setEditing(true);
        }}
      >
        {name}
      </button>
    </h1>
  );
}

// ---------------------------------------------------------------- description

// The "Edit description" button and the description body share state through
// a tiny event so the button can sit in the heading row, as in the design.
const editDescriptionEvent = "tracker:edit-description";

function DescriptionButton({ readOnly }: { readOnly: boolean }) {
  return (
    <button type="button" className="btn-secondary" disabled={readOnly} onClick={() => window.dispatchEvent(new Event(editDescriptionEvent))}>
      Edit description
    </button>
  );
}

function Description({ text, readOnly, onSave }: { text: string; readOnly: boolean; onSave: (v: string) => void }) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(text);
  useEffect(() => {
    const start = () => {
      setDraft(text);
      setEditing(true);
    };
    window.addEventListener(editDescriptionEvent, start);
    return () => window.removeEventListener(editDescriptionEvent, start);
  }, [text]);

  if (editing) {
    const save = () => {
      setEditing(false);
      onSave(draft.trim());
    };
    return (
      <div className="description-editor">
        <textarea
          className="field"
          aria-label="Description"
          autoFocus
          value={draft}
          disabled={readOnly}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && modKey(e)) {
              e.preventDefault();
              save();
            } else if (e.key === "Escape") {
              e.stopPropagation();
              setEditing(false);
            }
          }}
        />
        <div className="row-end">
          <button type="button" className="btn-secondary" onClick={() => setEditing(false)}>
            Cancel
          </button>
          <button type="button" className="btn-primary" disabled={readOnly} onClick={save}>
            Save
          </button>
        </div>
      </div>
    );
  }
  if (!text) return <p className="description description-empty">No description yet.</p>;
  return <p className="description">{text}</p>;
}

// ---------------------------------------------------------------- tags

function TagEditor({ tags, all, readOnly, onChange }: { tags: string[]; all: string[]; readOnly: boolean; onChange: (t: string[]) => void }) {
  const [draft, setDraft] = useState("");
  const [active, setActive] = useState(0);
  const [focused, setFocused] = useState(false);
  const input = useRef<HTMLInputElement>(null);

  const q = draft.trim().toLowerCase();
  const suggestions = q
    ? all.filter((t) => t.toLowerCase().includes(q) && !tags.some((x) => sameTag(x, t))).slice(0, 8)
    : [];
  const showList = focused && suggestions.length > 0;

  const add = (raw: string) => {
    const name = raw.trim().replace(/\s+/g, " ").slice(0, 40);
    setDraft("");
    setActive(0);
    if (!name || tags.some((x) => sameTag(x, name)) || tags.length >= MAX_TAGS) return;
    const existing = all.find((t) => sameTag(t, name));
    onChange([...tags, existing ?? name]);
  };

  return (
    <div className="tag-editor" aria-label="Tags">
      {tags.map((t) => (
        <span key={t} className="chip">
          {t}
          <button
            type="button"
            className="chip-remove"
            aria-label={`Remove tag ${t}`}
            disabled={readOnly}
            onClick={() => onChange(tags.filter((x) => x !== t))}
          >
            <CloseIcon />
          </button>
        </span>
      ))}
      {tags.length < MAX_TAGS && (
        <div className="tag-input-wrap">
          <input
            ref={input}
            className="tag-input"
            placeholder="Add tag"
            aria-label="Add tag"
            role="combobox"
            aria-expanded={showList}
            aria-controls="tag-suggestions"
            aria-autocomplete="list"
            aria-activedescendant={showList ? `tag-sugg-${active}` : undefined}
            maxLength={40}
            disabled={readOnly}
            value={draft}
            onFocus={() => setFocused(true)}
            onBlur={() => setFocused(false)}
            onChange={(e) => {
              const v = e.target.value;
              if (v.includes(",")) {
                v.split(",").slice(0, -1).forEach(add);
                setDraft(v.split(",").pop() ?? "");
              } else {
                setDraft(v);
                setActive(0);
              }
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                add(showList ? suggestions[active] : draft);
              } else if (e.key === "ArrowDown" && showList) {
                e.preventDefault();
                setActive((a) => (a + 1) % suggestions.length);
              } else if (e.key === "ArrowUp" && showList) {
                e.preventDefault();
                setActive((a) => (a - 1 + suggestions.length) % suggestions.length);
              } else if (e.key === "Escape") {
                e.stopPropagation();
                setDraft("");
                input.current?.blur();
              } else if (e.key === "Backspace" && draft === "" && tags.length > 0) {
                onChange(tags.slice(0, -1));
              }
            }}
          />
          {showList && (
            <ul className="suggestions" role="listbox" id="tag-suggestions">
              {suggestions.map((s, i) => (
                <li
                  key={s}
                  id={`tag-sugg-${i}`}
                  role="option"
                  aria-selected={i === active}
                  onMouseDown={(e) => {
                    e.preventDefault();
                    add(s);
                  }}
                >
                  {s}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------- log

function LogComposer({ readOnly, onAdd }: { readOnly: boolean; onAdd: (body: string) => Promise<void> }) {
  const [draft, setDraft] = useState("");
  const submit = () => {
    const body = draft.trim();
    if (!body) return;
    setDraft("");
    void onAdd(body);
  };
  return (
    <form
      className="composer-card"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <textarea
        aria-label="New log entry"
        rows={3}
        placeholder="What happened? Progress, a decision, something to remember…"
        value={draft}
        disabled={readOnly}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && modKey(e)) {
            e.preventDefault();
            submit();
          }
        }}
      />
      <div className="row-end">
        <button type="submit" className="btn-primary" disabled={readOnly || !draft.trim()}>
          Add entry
        </button>
      </div>
    </form>
  );
}

function Entry({ entry, readOnly, onSave, onDelete }: { entry: LogEntry; readOnly: boolean; onSave: (b: string) => void; onDelete: () => void }) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(entry.body);
  const save = () => {
    const v = draft.trim();
    setEditing(false);
    if (v) onSave(v);
  };
  return (
    <article className="entry">
      <span className="entry-date">{formatDate(entry.created_at)}</span>
      <div className="entry-body">
        {editing ? (
          <div className="stack">
            <textarea
              className="field"
              aria-label="Edit log entry"
              autoFocus
              rows={4}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && modKey(e)) {
                  e.preventDefault();
                  save();
                } else if (e.key === "Escape") {
                  e.stopPropagation();
                  setEditing(false);
                }
              }}
            />
            <div className="row-end">
              <button type="button" className="btn-secondary" onClick={() => setEditing(false)}>
                Cancel
              </button>
              <button type="button" className="btn-primary" disabled={readOnly || !draft.trim()} onClick={save}>
                Save
              </button>
            </div>
          </div>
        ) : (
          <>
            <p>{entry.body}</p>
            <Menu
              label="Log entry actions"
              disabled={readOnly}
              items={[
                {
                  label: "Edit entry",
                  onSelect: () => {
                    setDraft(entry.body);
                    setEditing(true);
                  },
                },
                { label: "Delete entry…", onSelect: onDelete },
              ]}
            />
          </>
        )}
      </div>
    </article>
  );
}

// ---------------------------------------------------------------- tasks

function AddTaskForm({ readOnly, onAdd }: { readOnly: boolean; onAdd: (title: string) => Promise<void> }) {
  const [draft, setDraft] = useState("");
  return (
    <form
      className="add-task-form"
      onSubmit={(e) => {
        e.preventDefault();
        const t = draft.trim();
        if (!t) return;
        setDraft("");
        void onAdd(t);
      }}
    >
      <input
        type="text"
        className="field"
        aria-label="Add a task"
        placeholder="Add a task…"
        maxLength={500}
        disabled={readOnly}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
      />
      <button type="submit" className="btn-secondary" aria-label="Add task" disabled={readOnly}>
        <PlusIcon />
      </button>
    </form>
  );
}
