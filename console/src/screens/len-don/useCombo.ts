// Ô gợi ý của màn Lên đơn (HV03): ↑ ↓ đổi dòng sáng, Enter chọn dòng sáng, Esc đóng danh sách, rời khối thì đóng.
// Dùng cho ô tìm khách, ô tìm sản phẩm, ô xã/phường.
import { useState, type ChangeEvent, type FocusEvent, type KeyboardEvent, type MouseEvent } from "react";

export interface Combo {
  /** Chỉ số dòng đang sáng (0 khi danh sách mới mở). */
  active: number;
  /** Thuộc tính của ô gõ; onText nhận chữ mới (danh sách mở lại, dòng sáng về đầu). */
  input: (onText: (s: string) => void) => {
    onFocus: () => void;
    onChange: (e: ChangeEvent<HTMLInputElement>) => void;
    onKeyDown: (e: KeyboardEvent<HTMLInputElement>) => void;
  };
  /** onBlur của khối bọc ô gõ và danh sách: focus ra ngoài khối thì đóng. */
  onBlur: (e: FocusEvent<HTMLElement>) => void;
  /** onMouseDown của danh sách: giữ focus ở ô gõ khi bấm một dòng (Safari đóng danh sách trước click). */
  keepFocus: (e: MouseEvent) => void;
}

/**
 * setOpen: trạng thái mở của danh sách (bên gọi giữ, vì số dòng hiện phụ thuộc nó). count: số dòng đang hiện (0: đóng
 * hoặc trống). onPick(i): chọn dòng i. Enter khi không có dòng nào chỉ chặn gửi form; Ctrl/⌘+Enter để cho phím lưu.
 */
export function useCombo(setOpen: (v: boolean) => void, count: number, onPick: (i: number) => void): Combo {
  const [active, setActive] = useState(0);
  const cur = count === 0 ? 0 : Math.min(active, count - 1);
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" && count > 0) {
      e.preventDefault();
      setActive((cur + 1) % count);
    } else if (e.key === "ArrowUp" && count > 0) {
      e.preventDefault();
      setActive((cur - 1 + count) % count);
    } else if (e.key === "Enter" && !e.ctrlKey && !e.metaKey) {
      e.preventDefault();
      if (count > 0) onPick(cur);
    } else if (e.key === "Escape") {
      setOpen(false);
    }
  };
  return {
    active: cur,
    input: (onText) => ({
      onFocus: () => {
        setOpen(true);
      },
      onChange: (e) => {
        onText(e.target.value);
        setOpen(true);
        setActive(0);
      },
      onKeyDown,
    }),
    onBlur: (e) => {
      if (!(e.relatedTarget instanceof Node && e.currentTarget.contains(e.relatedTarget))) setOpen(false);
    },
    keepFocus: (e) => {
      e.preventDefault();
    },
  };
}
