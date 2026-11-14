import { useEffect, useRef, useState } from "react";
import { api, errorMessage } from "../api";
import type { Session } from "../api";
import { useData } from "../data";
import { formatDate } from "../format";
import { InlineError, useInlineError } from "./InlineError";

/** Sync dot + label; opens the settings popover. */
export function SyncIndicator({ align }: { align: "left" | "right" }) {
  const { sync, status } = useData();
  const [open, setOpen] = useState(false);
  const wrap = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!wrap.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  const label = sync === "offline" ? "Offline" : sync === "syncing" ? "Syncing…" : `Synced · ${status.host}`;

  return (
    <div
      className="sync-wrap"
      ref={wrap}
      onKeyDown={(e) => {
        if (e.key === "Escape" && open) {
          e.stopPropagation();
          setOpen(false);
          button.current?.focus();
        }
      }}
    >
      <button
        ref={button}
        type="button"
        className="sync"
        data-state={sync}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={`${label}. Settings`}
        onClick={() => setOpen((o) => !o)}
      >
        <span className="sync-dot" aria-hidden="true" />
        <span aria-live="polite">{label}</span>
      </button>
      {open && <Settings align={align} />}
    </div>
  );
}

function Settings({ align }: { align: "left" | "right" }) {
  const { status, signOut, readOnly } = useData();
  const [sessions, setSessions] = useState<Session[] | null>(null);
  const [err, showErr] = useInlineError();

  useEffect(() => {
    api.listSessions().then(setSessions, showErr);
  }, [showErr]);

  const revoke = async (s: Session) => {
    const prev = sessions;
    setSessions((list) => list?.filter((x) => x.id !== s.id) ?? null);
    try {
      await api.revokeSession(s.id, s.current);
      if (s.current) await signOut();
    } catch (e) {
      setSessions(prev);
      showErr(e);
    }
  };

  return (
    <div className={`popover popover-${align}`} role="dialog" aria-label="Settings">
      <section>
        <h2 className="section-label">Server</h2>
        <div className="server-address">{status.server}</div>
      </section>

      <section>
        <h2 className="section-label">Signed-in devices</h2>
        {sessions === null ? (
          <p className="empty-note">Loading…</p>
        ) : (
          <ul className="device-list">
            {sessions.map((s) => (
              <li key={s.id} className="device">
                <div className="stack" style={{ gap: 2 }}>
                  <span className="device-name">
                    {s.device_name}
                    {s.current ? " (this device)" : ""}
                  </span>
                  <span className="device-meta">Last used {formatDate(s.last_used_at)}</span>
                </div>
                <button type="button" className="btn-secondary" disabled={readOnly} onClick={() => void revoke(s)}>
                  Revoke
                </button>
              </li>
            ))}
          </ul>
        )}
        <InlineError message={err} />
      </section>

      <ChangePassword />

      <section>
        <div>
          <button type="button" className="btn-secondary" onClick={() => void signOut()}>
            Sign out
          </button>
        </div>
      </section>
    </div>
  );
}

function ChangePassword() {
  const { readOnly } = useData();
  const [open, setOpen] = useState(false);
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  if (!open) {
    return (
      <section>
        <h2 className="section-label">Password</h2>
        <div>
          <button type="button" className="btn-secondary" disabled={readOnly} onClick={() => setOpen(true)}>
            Change password
          </button>
        </div>
        {done && <p className="empty-note">Password changed. Other devices were signed out.</p>}
      </section>
    );
  }
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setMsg(null);
    try {
      await api.changePassword(current, next);
      setOpen(false);
      setCurrent("");
      setNext("");
      setDone(true);
    } catch (err) {
      setMsg(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section>
      <h2 className="section-label">Change password</h2>
      <form className="stack" onSubmit={submit}>
        <input
          className="field"
          type="password"
          aria-label="Current password"
          placeholder="Current password"
          autoComplete="current-password"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          required
        />
        <input
          className="field"
          type="password"
          aria-label="New password"
          placeholder="New password (at least 12 characters)"
          autoComplete="new-password"
          minLength={12}
          value={next}
          onChange={(e) => setNext(e.target.value)}
          required
        />
        {msg && <p className="inline-error" role="alert">{msg}</p>}
        <div className="row-end">
          <button type="button" className="btn-secondary" onClick={() => setOpen(false)}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={busy || readOnly}>
            {busy ? "Saving…" : "Save"}
          </button>
        </div>
      </form>
    </section>
  );
}
