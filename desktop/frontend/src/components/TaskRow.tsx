import { useState } from "react";
import type { Task } from "../api";
import { CheckIcon } from "./Icons";
import { Menu } from "./Menu";
import type { MenuItem } from "./Menu";

type Props = {
  task: Task;
  large?: boolean;
  readOnly: boolean;
  onToggle: () => void;
  onRename?: (title: string) => void;
  onDelete?: () => void;
};

/** A task: a real toggle button with a 16 px checkbox, plus an optional "…" menu. */
export function TaskRow({ task, large, readOnly, onToggle, onRename, onDelete }: Props) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(task.title);

  if (editing && onRename) {
    const save = () => {
      const t = draft.trim();
      setEditing(false);
      if (t && t !== task.title) onRename(t);
    };
    return (
      <div className="task-row">
        <div className="task-edit">
          <input
            className="field"
            aria-label="Task title"
            value={draft}
            autoFocus
            maxLength={500}
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
        </div>
      </div>
    );
  }

  const items: MenuItem[] = [];
  if (onRename)
    items.push({
      label: "Edit task",
      onSelect: () => {
        setDraft(task.title);
        setEditing(true);
      },
    });
  if (onDelete) items.push({ label: "Delete task…", onSelect: onDelete });

  return (
    <div className="task-row">
      <button
        type="button"
        className={large ? "task task-lg" : "task"}
        aria-pressed={task.done}
        disabled={readOnly}
        onClick={onToggle}
      >
        <span className="checkbox" aria-hidden="true">
          {task.done && <CheckIcon />}
        </span>
        <span>{task.title}</span>
      </button>
      {items.length > 0 && <Menu label={`Actions for task ${task.title}`} items={items} disabled={readOnly} />}
    </div>
  );
}
