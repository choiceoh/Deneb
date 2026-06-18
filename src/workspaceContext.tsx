// Workstation state shared across the three columns, and the mechanism by which
// the active pane publishes its content to the AI panel.
//
// The "well-structured back end" principle (workspace.ts): the active pane
// serializes its current content to TEXT and PUSHES it here; the AI panel reads
// the latest text — no vision, no cross-pane data coupling. Only the active pane
// is mounted (Workstation renders one at a time), so whoever is mounted owns the
// AI context and names the resource the AI's tool calls should invalidate.
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import type { View } from "./types";

interface WorkspaceCtx {
  connected: boolean;
  view: View;
  setView: (v: View) => void;
  // `doc` lives here (not in the pane) so the scratch document survives pane
  // switches even though the DocPane unmounts.
  doc: string;
  setDoc: (s: string) => void;
  // Pushed by the active pane: its content serialized for the AI, and the Refine
  // resource (if any) backing it — used to refresh after the AI mutates data.
  aiText: string;
  activeResource?: string;
  registerPane: (resource: string | undefined, text: string) => void;
}

const Ctx = createContext<WorkspaceCtx | null>(null);

export function WorkspaceProvider({ connected, children }: { connected: boolean; children: ReactNode }) {
  const [view, setView] = useState<View>("todo");
  const [doc, setDoc] = useState("");
  const [aiText, setAiText] = useState("");
  const [activeResource, setActiveResource] = useState<string | undefined>(undefined);

  const registerPane = (resource: string | undefined, t: string) => {
    setActiveResource(resource);
    setAiText(t);
  };

  return (
    <Ctx.Provider value={{ connected, view, setView, doc, setDoc, aiText, activeResource, registerPane }}>
      {children}
    </Ctx.Provider>
  );
}

export function useWorkspace(): WorkspaceCtx {
  const c = useContext(Ctx);
  if (!c) throw new Error("useWorkspace must be used within <WorkspaceProvider>");
  return c;
}

// Called by the active pane to publish its AI-text projection and backing
// resource whenever its content changes.
export function useRegisterPane(resource: string | undefined, text: string): void {
  const { registerPane } = useWorkspace();
  useEffect(() => {
    registerPane(resource, text);
    // registerPane is stable enough; re-run when the projection or resource changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resource, text]);
}
