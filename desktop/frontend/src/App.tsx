import { useCallback, useEffect, useState } from "react";
import { api } from "./api";
import type { Status } from "./api";
import { DataProvider } from "./data";
import { Archived } from "./components/Archived";
import { Board } from "./components/Board";
import { ConfirmProvider } from "./components/Confirm";
import { Connect } from "./components/Connect";
import { ProjectPage } from "./components/ProjectPage";

type Screen = { name: "board" } | { name: "project"; id: string } | { name: "archived" };

export function App() {
  const [status, setStatus] = useState<Status | null>(null);
  const [screen, setScreen] = useState<Screen>({ name: "board" });

  const loadStatus = useCallback(() => {
    void api.status().then((s) => {
      setStatus(s);
      document.title = s.app_name;
    });
  }, []);
  useEffect(loadStatus, [loadStatus]);

  // On a 401 the Go side has already deleted the stored token.
  const onSignedOut = useCallback(() => {
    setScreen({ name: "board" });
    loadStatus();
  }, [loadStatus]);

  const toBoard = useCallback(() => setScreen({ name: "board" }), []);
  const openProject = useCallback((id: string) => setScreen({ name: "project", id }), []);
  const openArchived = useCallback(() => setScreen({ name: "archived" }), []);

  if (!status) return null;
  if (!status.connected) return <Connect status={status} onConnected={loadStatus} />;

  return (
    <DataProvider status={status} onSignedOut={onSignedOut}>
      <ConfirmProvider>
        {screen.name === "board" && <Board onOpen={openProject} onArchived={openArchived} />}
        {screen.name === "project" && <ProjectPage key={screen.id} id={screen.id} onBack={toBoard} />}
        {screen.name === "archived" && <Archived onBack={toBoard} />}
      </ConfirmProvider>
    </DataProvider>
  );
}
