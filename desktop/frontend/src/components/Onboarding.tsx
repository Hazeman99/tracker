import { useEffect, useRef, useState } from "react";
import { isMac } from "../format";

type Step = { title: string; body: string };

function steps(appName: string): Step[] {
  const mod = isMac ? "Cmd" : "Ctrl";
  return [
    {
      title: `Welcome to ${appName}`,
      body:
        "Your projects live on the board, one column each. Click a project's name to open its page, with the full " +
        "description, every log entry and all of its tasks.",
    },
    {
      title: "Tasks",
      body:
        "Type in “Add a task…” and press Enter. Check a task off and it fades from the board after a few seconds; " +
        "finished tasks stay on the project page under Done.",
    },
    {
      title: "Log",
      body:
        "Use “Write log entry” at the bottom of a column to note progress or a decision. " +
        `${mod}+Enter saves, Esc cancels. The newest entries show first.`,
    },
    {
      title: "Tags",
      body:
        "Add tags such as “Freelance” or “Mobile App” on a project's page. The chips above the board filter it to " +
        "projects that have any of the selected tags.",
    },
    {
      title: "Make it yours",
      body:
        `Create a project with “New project” or ${mod}+N, and drag a column's header to reorder the board. ` +
        "Click the sync status at the top to manage signed-in devices, change your password or sign out.",
    },
  ];
}

/** A short first-run tutorial, shown once per device after signing in. */
export function Onboarding({ appName, onDone }: { appName: string; onDone: () => void }) {
  const all = steps(appName);
  const [i, setI] = useState(0);
  const dialog = useRef<HTMLDialogElement>(null);
  const next = useRef<HTMLButtonElement>(null);
  const last = i === all.length - 1;

  useEffect(() => {
    if (dialog.current && !dialog.current.open) dialog.current.showModal();
  }, []);
  useEffect(() => {
    next.current?.focus();
  }, [i]);

  const finish = () => {
    dialog.current?.close();
    onDone();
  };

  return (
    <dialog
      ref={dialog}
      className="confirm onboarding"
      aria-labelledby="onboarding-title"
      aria-describedby="onboarding-body"
      onCancel={(e) => {
        e.preventDefault();
        finish();
      }}
      onKeyDown={(e) => {
        e.stopPropagation();
        if (e.key === "ArrowRight" && !last) setI(i + 1);
        if (e.key === "ArrowLeft" && i > 0) setI(i - 1);
      }}
    >
      <div className="onboarding-head">
        <span className="count" aria-live="polite">
          {i + 1} / {all.length}
        </span>
        <button type="button" className="btn-text" onClick={finish}>
          Skip
        </button>
      </div>
      <h2 id="onboarding-title">{all[i].title}</h2>
      <p id="onboarding-body">{all[i].body}</p>
      <div className="onboarding-foot">
        <div className="onboarding-dots" aria-hidden="true">
          {all.map((s, n) => (
            <span key={s.title} data-active={n === i} />
          ))}
        </div>
        <div className="row-end">
          {i > 0 && (
            <button type="button" className="btn-secondary" onClick={() => setI(i - 1)}>
              Back
            </button>
          )}
          <button ref={next} type="button" className="btn-primary" onClick={() => (last ? finish() : setI(i + 1))}>
            {last ? "Get started" : "Next"}
          </button>
        </div>
      </div>
    </dialog>
  );
}
