// Ô giờ (datetime-local, giờ của máy) có câu lỗi tại ô, dùng cho "Nhắc tôi lúc…" và "Gọi lại lúc" (Task 176).
import { useId } from "react";

export interface TimeFieldProps {
  label: string;
  value: string;
  /** Câu lỗi tại ô; null khi không lỗi. */
  error: string | null;
  onChange: (v: string) => void;
}

export function TimeField({ label, value, error, onChange }: TimeFieldProps) {
  const id = useId();
  return (
    <>
      <label className="field">
        <span>{label}</span>
        <input
          className="input"
          type="datetime-local"
          aria-invalid={error !== null}
          aria-describedby={error === null ? undefined : `${id}-err`}
          value={value}
          onChange={(e) => {
            onChange(e.target.value);
          }}
        />
      </label>
      {error !== null && (
        <p className="err-text" id={`${id}-err`}>
          {error}
        </p>
      )}
    </>
  );
}
