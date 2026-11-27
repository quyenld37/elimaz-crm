// Chọn kỳ báo cáo (Task 239, M6-16; mockup tong-quan.html: .period, trạng thái A, C). Năm lựa chọn; Tùy chọn mở hai ô
// ngày và nút Xem; quá 366 ngày hay ngày tương lai khóa nút Xem và nói rõ lý do (luật ở period.ts).
import { useState } from "react";
import { periodError, presetPeriod, type Period, type PeriodPreset } from "./period";

const OPTIONS: readonly { preset: PeriodPreset; label: string }[] = [
  { preset: "today", label: "Hôm nay" },
  { preset: "yesterday", label: "Hôm qua" },
  { preset: "7d", label: "7 ngày" },
  { preset: "month", label: "Tháng này" },
  { preset: "custom", label: "Tùy chọn" },
];

export interface PeriodPickerProps {
  value: Period;
  /** Hôm nay theo giờ Việt Nam (YYYY-MM-DD). */
  today: string;
  onChange: (p: Period) => void;
  /** Đang tải: khóa mọi nút. */
  disabled?: boolean;
}

export function PeriodPicker({ value, today, onChange, disabled = false }: PeriodPickerProps) {
  const [editing, setEditing] = useState(value.preset === "custom");
  const [draft, setDraft] = useState({ from: value.from, to: value.to });
  const error = periodError(draft, today);
  const customOn = editing || value.preset === "custom";

  const pick = (preset: PeriodPreset) => {
    if (preset === "custom") {
      setEditing(true);
      return;
    }
    setEditing(false);
    onChange(presetPeriod(preset, today));
  };

  return (
    <div className="stack-sm">
      <div className="period" role="group" aria-label="Chọn kỳ">
        {OPTIONS.map((o) => (
          <button
            key={o.preset}
            type="button"
            className="toggle"
            aria-pressed={o.preset === "custom" ? customOn : !customOn && value.preset === o.preset}
            disabled={disabled}
            onClick={() => {
              pick(o.preset);
            }}
          >
            {o.label}
          </button>
        ))}
      </div>
      {customOn && (
        <div className="period">
          <div className="dates">
            <input
              type="date"
              className={error === null ? "input num" : "input num is-err"}
              aria-label="Từ ngày"
              value={draft.from}
              max={today}
              disabled={disabled}
              onChange={(e) => {
                setDraft({ ...draft, from: e.target.value });
              }}
            />
            <span className="muted">→</span>
            <input
              type="date"
              className={error === null ? "input num" : "input num is-err"}
              aria-label="Đến ngày"
              value={draft.to}
              max={today}
              disabled={disabled}
              onChange={(e) => {
                setDraft({ ...draft, to: e.target.value });
              }}
            />
            <button
              type="button"
              className="btn btn-dark"
              disabled={disabled || error !== null}
              onClick={() => {
                onChange({ preset: "custom", from: draft.from, to: draft.to });
              }}
            >
              Xem
            </button>
          </div>
          {error !== null && <p className="field-err">{error}</p>}
        </div>
      )}
    </div>
  );
}
