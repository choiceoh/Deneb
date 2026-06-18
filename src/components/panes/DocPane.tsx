import { field } from "../../theme";
import { useRegisterPane, useWorkspace } from "../../workspaceContext";

// Client-only scratch document. Its text lives in the workspace context so it
// survives pane switches, and it has no backing resource (AI reads it but there's
// nothing to invalidate after a tool call).
export function DocPane() {
  const { doc, setDoc } = useWorkspace();
  useRegisterPane(undefined, doc.trim() ? `[문서]\n${doc}` : "");

  return (
    <>
      <h2 style={{ marginTop: 2 }}>문서</h2>
      <textarea
        value={doc}
        onChange={(e) => setDoc(e.target.value)}
        placeholder="여기에 문서를 작성하세요. 우측 AI가 이 내용을 텍스트로 읽습니다."
        style={{
          ...field,
          width: "100%",
          height: "70vh",
          boxSizing: "border-box",
          resize: "none",
          fontFamily: "ui-monospace, monospace",
          fontSize: 13,
          lineHeight: 1.5,
        }}
      />
    </>
  );
}
