// The AI panel's model/service picker. Lists miniapp.models.list grouped by
// section (provider/role); selecting one drives the per-turn `model` override
// sent to chat/stream. Renders nothing until models load, so the header stays
// clean when disconnected or on a gateway without the models surface.
import type { ModelsList } from "@/gateway";

export function ModelPicker({
  models,
  value,
  onChange,
  disabled,
}: {
  models: ModelsList | null;
  value: string;
  onChange: (id: string) => void;
  disabled?: boolean;
}) {
  if (!models || models.sections.length === 0) return null;

  // Surface the active id even if it isn't in a section (custom/older models),
  // so the <select> never renders blank.
  const known = new Set(models.sections.flatMap((s) => s.models.map((m) => m.id)));
  const orphan = value && !known.has(value) ? value : null;

  return (
    <select
      className="model-picker field"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled}
      aria-label="모델 선택"
      title="대화에 사용할 모델"
    >
      {orphan ? <option value={orphan}>{orphan}</option> : null}
      {models.sections.map((s) => (
        <optgroup key={s.title} label={s.title}>
          {s.models.map((m) => (
            <option key={m.id} value={m.id}>
              {m.label}
              {m.unhealthy ? " ⚠" : ""}
            </option>
          ))}
        </optgroup>
      ))}
    </select>
  );
}
