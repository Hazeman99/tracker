import { useEffect } from "react";
import { api } from "../api";
import { useData } from "../data";
import { formatDate } from "../format";
import { BackIcon } from "./Icons";
import { InlineError, useInlineError } from "./InlineError";
import { SyncIndicator } from "./Sync";

/** Lists archived projects and restores them. */
export function Archived({ onBack }: { onBack: () => void }) {
  const { projects, updateProject, readOnly } = useData();
  const [err, showErr] = useInlineError();
  const archived = projects.filter((p) => p.archived_at);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented && !(e.target as HTMLElement).closest(".popover")) onBack();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onBack]);

  const restore = async (id: string, archivedAt: string | null) => {
    updateProject(id, (p) => ({ ...p, archived_at: null }));
    try {
      await api.updateProject(id, { archived: false });
    } catch (e) {
      updateProject(id, (p) => ({ ...p, archived_at: archivedAt }));
      showErr(e);
    }
  };

  return (
    <div className="app">
      <header className="topbar">
        <button type="button" className="back-link" onClick={onBack}>
          <BackIcon />
          <span>All projects</span>
        </button>
        <SyncIndicator align="right" />
      </header>
      <div className="page-scroll">
        <div className="archived-page">
          <h1 className="page-title">Archived</h1>
          <InlineError message={err} />
          {archived.length === 0 ? (
            <p className="empty-note">No archived projects.</p>
          ) : (
            <ul className="archived-list">
              {archived.map((p) => (
                <li key={p.id} className="archived-item">
                  <div className="stack" style={{ gap: 2 }}>
                    <span className="archived-name">{p.name}</span>
                    <span className="count">Archived {formatDate(p.archived_at!)}</span>
                  </div>
                  <button type="button" className="btn-secondary" disabled={readOnly} onClick={() => void restore(p.id, p.archived_at)}>
                    Restore
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
