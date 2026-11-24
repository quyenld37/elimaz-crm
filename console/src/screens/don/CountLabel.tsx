// Nút nhãn lọc của thanh nhãn (.lblbar, .lbl của don.css) có số cạnh chữ (.lbl .n; mockup M5 order-requests.html,
// feedback.html; Task 486). n chưa có (đang đọc, lỗi) → chỉ chữ. Tên đọc được: "Đang mở 8".
export interface CountLabelProps {
  text: string;
  n: number | undefined;
  pressed: boolean;
  attention?: boolean;
  onSelect: () => void;
}

export function CountLabel({ text, n, pressed, attention = false, onSelect }: CountLabelProps) {
  return (
    <button type="button" className={attention ? "lbl is-attn" : "lbl"} aria-pressed={pressed} onClick={onSelect}>
      {text}
      {n !== undefined && (
        <>
          {" "}
          <span className="n">{n}</span>
        </>
      )}
    </button>
  );
}
