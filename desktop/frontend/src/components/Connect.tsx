import { useState } from "react";
import { api, errorMessage } from "../api";
import type { Status } from "../api";

/** First-run / signed-out screen. */
export function Connect({ status, onConnected }: { status: Status; onConnected: () => void }) {
  const [server, setServer] = useState(status.server || "");
  const [email, setEmail] = useState(status.email || "");
  const [password, setPassword] = useState("");
  const [insecureTLS, setInsecureTLS] = useState(!!status.insecure_tls);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      await api.connect(server, email, password, insecureTLS);
      setPassword("");
      onConnected();
    } catch (e2) {
      setErr(errorMessage(e2).replace("Offline. The change was not saved.", "Cannot reach the server."));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="connect">
      <form onSubmit={submit} noValidate>
        <div className="stack">
          <h1>{status.app_name}</h1>
          <p className="lead">Connect this computer to your server. You only do this once per device.</p>
        </div>
        <div className="connect-fields">
          <label>
            <span>Server address</span>
            <input
              className="mono"
              type="url"
              placeholder="https://tracker.example.com"
              autoComplete="url"
              spellCheck={false}
              autoCapitalize="off"
              value={server}
              onChange={(e) => setServer(e.target.value)}
              required
              autoFocus={!status.server}
            />
          </label>
          <label>
            <span>Email</span>
            <input
              type="email"
              placeholder="you@example.com"
              autoComplete="username"
              spellCheck={false}
              autoCapitalize="off"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
          </label>
          <label>
            <span>Password</span>
            <input
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              autoFocus={!!status.server}
            />
          </label>
          <label className="insecure-tls">
            <input type="checkbox" checked={insecureTLS} onChange={(e) => setInsecureTLS(e.target.checked)} />
            <span>Disable SSL certificate verification</span>
          </label>
          {insecureTLS && (
            <p className="insecure-warning" role="alert">
              The server's identity will not be verified. Only use this with a server you trust, such as one with a
              self-signed certificate.
            </p>
          )}
          {err && (
            <p className="inline-error" role="alert">
              {err}
            </p>
          )}
        </div>
        <button type="submit" className="btn-primary" disabled={busy || !server || !email || !password}>
          {busy ? "Connecting…" : "Connect"}
        </button>
      </form>
    </div>
  );
}
