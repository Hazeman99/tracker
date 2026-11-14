import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";

type Request = { title: string; body: string; confirmLabel: string; resolve: (ok: boolean) => void };

const Ctx = createContext<(title: string, body: string, confirmLabel?: string) => Promise<boolean>>(
  () => Promise.resolve(false),
);

/** Promise-based confirmation dialog for destructive actions. */
export function useConfirm() {
  return useContext(Ctx);
}

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [req, setReq] = useState<Request | null>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const returnFocus = useRef<HTMLElement | null>(null);

  const confirm = useCallback(
    (title: string, body: string, confirmLabel = "Delete") =>
      new Promise<boolean>((resolve) => {
        returnFocus.current = document.activeElement as HTMLElement | null;
        setReq({ title, body, confirmLabel, resolve });
      }),
    [],
  );

  useEffect(() => {
    if (req && dialog.current && !dialog.current.open) dialog.current.showModal();
  }, [req]);

  const finish = (ok: boolean) => {
    req?.resolve(ok);
    dialog.current?.close();
    setReq(null);
    returnFocus.current?.focus?.();
  };

  return (
    <Ctx.Provider value={confirm}>
      {children}
      {req && (
        <dialog
          ref={dialog}
          className="confirm"
          aria-labelledby="confirm-title"
          onCancel={(e) => {
            e.preventDefault();
            finish(false);
          }}
          onKeyDown={(e) => e.stopPropagation()}
        >
          <h2 id="confirm-title">{req.title}</h2>
          <p>{req.body}</p>
          <div className="row-end">
            <button type="button" className="btn-secondary" onClick={() => finish(false)} autoFocus>
              Cancel
            </button>
            <button type="button" className="btn-primary" onClick={() => finish(true)}>
              {req.confirmLabel}
            </button>
          </div>
        </dialog>
      )}
    </Ctx.Provider>
  );
}
