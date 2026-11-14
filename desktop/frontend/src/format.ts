const sameYear = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric" });
const otherYear = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", year: "numeric" });

/** "Nov 14", or "Nov 14, 2025" for other years. */
export function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.getFullYear() === new Date().getFullYear() ? sameYear.format(d) : otherYear.format(d);
}

export function taskCounts(tasks: { done: boolean }[]): string {
  const done = tasks.filter((t) => t.done).length;
  return `${tasks.length - done} open · ${done} done`;
}

export const isMac = /Mac/i.test(navigator.platform || navigator.userAgent);

/** Cmd on macOS, Ctrl elsewhere. */
export function modKey(e: { metaKey: boolean; ctrlKey: boolean }): boolean {
  return isMac ? e.metaKey : e.ctrlKey;
}

export function sameTag(a: string, b: string): boolean {
  return a.localeCompare(b, undefined, { sensitivity: "accent" }) === 0;
}
