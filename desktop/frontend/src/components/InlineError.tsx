import { useCallback, useEffect, useState } from "react";
import { errorMessage } from "../api";

/** A small, self-clearing inline error for a failed (rolled back) change. */
export function useInlineError(): [string | null, (err: unknown) => void, () => void] {
  const [msg, setMsg] = useState<string | null>(null);
  useEffect(() => {
    if (!msg) return;
    const id = window.setTimeout(() => setMsg(null), 6000);
    return () => window.clearTimeout(id);
  }, [msg]);
  const show = useCallback((err: unknown) => setMsg(errorMessage(err)), []);
  const clear = useCallback(() => setMsg(null), []);
  return [msg, show, clear];
}

export function InlineError({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <p className="inline-error" role="alert">
      {message}
    </p>
  );
}
