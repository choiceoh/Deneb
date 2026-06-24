import type { Person } from "@/types";
import { fmtDate } from "@/format";
import { ellipsis } from "@/theme";
import { useListPane } from "@/useListPane";
import { useWorkspace } from "@/workspaceContext";
import { Column, Grid, GridNotice } from "@/components/Grid";
import { Detail, Modal } from "@/components/Modal";

// people.list merges recent Gmail counterparties (ranked by volume) with 인물 wiki
// pages — so a row may have a messageCount, a wikiSummary, or both.
export function PeoplePane() {
  const {
    rows: people,
    query,
    selected,
    setSelected,
  } = useListPane<Person>(
    "people",
    "연락처",
    (p) =>
      `- ${p.name ?? p.email}${p.email && p.name ? ` <${p.email}>` : ""}` +
      `${p.wikiSummary ? ` · ${p.wikiSummary}` : ""}` +
      `${p.lastSubject ? ` · 최근: ${p.lastSubject}` : ""}`,
    "명",
  );

  const columns: Column<Person>[] = [
    { header: "이름", width: 120, cell: (p) => p.name ?? "—" },
    {
      header: "이메일",
      width: 210,
      tdStyle: { fontSize: 13, opacity: 0.75, ...ellipsis(210) },
      cell: (p) => p.email ?? "",
    },
    {
      header: "메일",
      width: 56,
      tdStyle: { fontSize: 13, opacity: 0.7, textAlign: "right", whiteSpace: "nowrap" },
      cell: (p) => (p.messageCount ? String(p.messageCount) : ""),
    },
    {
      header: "최근 제목 / 메모",
      tdStyle: { fontSize: 13, opacity: 0.7 },
      cell: (p) => p.lastSubject ?? p.wikiSummary ?? "",
    },
    {
      header: "최근",
      width: 116,
      tdStyle: { fontSize: 13, opacity: 0.6, whiteSpace: "nowrap" },
      cell: (p) => fmtDate(p.lastSeen),
    },
  ];

  return (
    <>
      <h2 style={{ marginTop: 2 }}>연락처</h2>
      <GridNotice query={query} count={people.length} empty="연락처가 없습니다.">
        <Grid
          columns={columns}
          rows={people}
          getKey={(p) => p.email || String(p.id ?? "")}
          onRowClick={(p) => setSelected(p)}
        />
      </GridNotice>
      {selected && <PersonCard person={selected} onClose={() => setSelected(null)} />}
    </>
  );
}

// Person detail card. Surfaces the merged Gmail/wiki facts and, when the person has
// a 인물 wiki page, jumps to it via the shared openWiki channel (workspaceContext).
function PersonCard({ person, onClose }: { person: Person; onClose: () => void }) {
  const { openWiki } = useWorkspace();
  const openPage = () => {
    if (person.wikiPath) {
      openWiki(person.wikiPath);
      onClose();
    }
  };
  return (
    <Modal
      title={person.name ?? person.email}
      onClose={onClose}
      footer={
        <>
          {person.wikiPath && (
            <button className="btn btn-accent" onClick={openPage} style={{ marginRight: "auto" }}>
              위키 열기
            </button>
          )}
          <button className="btn" onClick={onClose}>
            닫기
          </button>
        </>
      }
    >
      {person.email && <Detail label="이메일" value={person.email} />}
      {person.messageCount != null && <Detail label="주고받은 메일" value={`${person.messageCount}건`} />}
      {person.lastSeen && <Detail label="최근 연락" value={fmtDate(person.lastSeen)} />}
      {person.lastSubject && <Detail label="최근 제목" value={person.lastSubject} />}
      {person.wikiSummary && <Detail label="위키 메모" value={person.wikiSummary} multiline />}
    </Modal>
  );
}
