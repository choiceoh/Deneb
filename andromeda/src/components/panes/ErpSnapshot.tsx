// Lays out a parsed Amaranth ERP snapshot. The snapshot is structured data that
// merely arrives as text (see erpText.ts) — a summary line, "레이블: 값" pairs, a
// section header, ranked rows of "·"-separated fields, and a provenance note.
// Markdown rendered all of that as prose: the pair lines merged into one run-on
// paragraph and each ranked row stayed a single long string. Here each part gets
// the shape it already had in the data.
import { parseErpText, type ErpBlock } from "@/erpText";

// The leading field of a ranked row is the item/title; the rest are attributes.
// Attributes are shown as quiet chips so the eye lands on the name and the
// numbers stay scannable down the column.
function ErpRow({ block }: { block: Extract<ErpBlock, { kind: "row" }> }) {
  return (
    <li className="erp-row">
      <span className="erp-rank">{block.rank}</span>
      <div className="erp-row-body">
        <div className="erp-row-name">{block.name}</div>
        {block.fields.length > 0 && (
          <div className="erp-row-fields">
            {block.fields.map((f, i) => (
              <span key={i} className="erp-field">
                {f}
              </span>
            ))}
          </div>
        )}
      </div>
    </li>
  );
}

export function ErpSnapshot({ text }: { text: string }) {
  const blocks = parseErpText(text);
  if (!blocks.length) return null;

  // Ranked rows are consecutive; group them so they render as one list rather
  // than a run of orphan <li> elements.
  const out: React.ReactNode[] = [];
  for (let i = 0; i < blocks.length; i++) {
    const b = blocks[i];
    if (b.kind === "row") {
      const rows: Extract<ErpBlock, { kind: "row" }>[] = [];
      while (i < blocks.length && blocks[i].kind === "row") {
        rows.push(blocks[i] as Extract<ErpBlock, { kind: "row" }>);
        i++;
      }
      i--;
      out.push(
        <ol className="erp-rows" key={`rows-${i}`}>
          {rows.map((r, j) => (
            <ErpRow block={r} key={j} />
          ))}
        </ol>,
      );
      continue;
    }
    if (b.kind === "title") {
      out.push(
        <h3 className="erp-title" key={i}>
          {b.text}
        </h3>,
      );
      continue;
    }
    if (b.kind === "meta") {
      out.push(
        <dl className="erp-meta" key={i}>
          {b.pairs.map((p, j) => (
            <div className="erp-meta-item" key={j}>
              <dt>{p.label}</dt>
              <dd>{p.value}</dd>
            </div>
          ))}
        </dl>,
      );
      continue;
    }
    if (b.kind === "section") {
      out.push(
        <h4 className="erp-section" key={i}>
          {b.text}
        </h4>,
      );
      continue;
    }
    out.push(
      <p className="erp-note" key={i}>
        {b.text}
      </p>,
    );
  }

  return <div className="erp-snapshot">{out}</div>;
}
