import { useCallback, useEffect, useRef, useState } from "react";

/** How long a just-checked task stays visible before it is hidden. */
export const DONE_LINGER_MS = 3000;

/**
 * Tracks tasks that were just checked: they stay visible for DONE_LINGER_MS,
 * then drop out of the list (on the board they disappear; on the project
 * page they move into the collapsed "Done" section).
 */
export function useLingeringDone() {
  const [ids, setIds] = useState<ReadonlySet<string>>(() => new Set());
  const timers = useRef(new Map<string, number>());

  useEffect(() => {
    const t = timers.current;
    return () => t.forEach((h) => window.clearTimeout(h));
  }, []);

  const stop = useCallback((id: string) => {
    const h = timers.current.get(id);
    if (h !== undefined) window.clearTimeout(h);
    timers.current.delete(id);
    setIds((prev) => {
      if (!prev.has(id)) return prev;
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
  }, []);

  const start = useCallback(
    (id: string) => {
      const old = timers.current.get(id);
      if (old !== undefined) window.clearTimeout(old);
      setIds((prev) => new Set(prev).add(id));
      timers.current.set(id, window.setTimeout(() => stop(id), DONE_LINGER_MS));
    },
    [stop],
  );

  return { lingering: ids, start, stop };
}
