import { useEffect, useState } from "react";

import { type GatewayConfig, codeRepos, codeStart } from "@/gateway";
import { errText } from "@/format";
import type { CodeSession } from "@/types";
import { Field, Modal, ModalFooter } from "@/components/Modal";

interface RepoOption {
  owner?: string;
  name?: string;
}

// CodeNewTaskModal — 왼쪽 레일의 "새 작업" 버튼이 띄우는 모달. 레포만 고르면(또는 gh 미인증 시
// owner/repo 직접 입력) 새 워크트리 작업을 만든다. taskId·title은 서버가 자동 생성한다. 예전엔
// 우측 패널에 상주하던 생성 폼을, 작업을 만들 때만 뜨는 모달로 옮긴 것 — 우측은 작업 상세 전용.
export function CodeNewTaskModal({
  cfg,
  onClose,
  onCreated,
}: {
  cfg: GatewayConfig;
  onClose: () => void;
  onCreated: (session: CodeSession) => void;
}) {
  const [repos, setRepos] = useState<RepoOption[]>([]);
  const [owner, setOwner] = useState("");
  const [repo, setRepo] = useState("");
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");

  useEffect(() => {
    codeRepos(cfg)
      .then(setRepos)
      .catch(() => setRepos([]));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function submit() {
    if (busy) return;
    if (!owner.trim() || !repo.trim()) {
      setStatus("레포를 선택하세요");
      return;
    }
    setBusy(true);
    setStatus("");
    try {
      // taskId·title은 서버가 자동 생성 — 레포만 있으면 된다.
      const sess = await codeStart(cfg, owner.trim(), repo.trim(), "");
      onCreated(sess);
    } catch (e) {
      setStatus(errText(e));
      setBusy(false);
    }
  }

  const hasRepos = repos.length > 0;
  const canSubmit = !busy && !!owner.trim() && !!repo.trim();

  return (
    <Modal
      title="새 작업"
      width={420}
      onClose={onClose}
      footer={
        <ModalFooter
          action={busy ? "만드는 중…" : "새 작업 만들기"}
          canSubmit={canSubmit}
          status={status}
          onClose={onClose}
          onSubmit={() => void submit()}
        />
      }
    >
      {hasRepos ? (
        <Field label="레포">
          <select
            className="field"
            value={owner && repo ? `${owner}/${repo}` : ""}
            onChange={(e) => {
              const [o, n] = e.target.value.split("/");
              setOwner(o ?? "");
              setRepo(n ?? "");
            }}
          >
            <option value="">레포 선택…</option>
            {repos.map((r) => {
              const v = `${r.owner ?? ""}/${r.name ?? ""}`;
              return (
                <option key={v} value={v}>
                  {v}
                </option>
              );
            })}
          </select>
        </Field>
      ) : (
        <Field label="레포 (owner / repo)">
          <div style={{ display: "flex", gap: 6 }}>
            <input
              className="field"
              style={{ flex: 1, minWidth: 0 }}
              placeholder="owner"
              value={owner}
              onChange={(e) => setOwner(e.target.value)}
            />
            <input
              className="field"
              style={{ flex: 1, minWidth: 0 }}
              placeholder="repo"
              value={repo}
              onChange={(e) => setRepo(e.target.value)}
            />
          </div>
        </Field>
      )}
    </Modal>
  );
}
