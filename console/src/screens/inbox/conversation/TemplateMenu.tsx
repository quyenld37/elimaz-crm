// Menu mẫu trả lời "/" ngay trên ô gõ (Task 175; mockup hoi-thoai.html trạng thái D; CH07; HV18). Hỏi listReplyTemplates
// theo chữ sau dấu "/" và Page của hội thoại (mẫu chung + mẫu Page đó). Ô gõ giữ phím: ↑ ↓ chọn, Enter chèn, Esc đóng
// (HV17 sửa: đang mở menu thì Enter chọn mẫu, không gửi).
import { useQuery } from "@tanstack/react-query";
import { listReplyTemplates, type ReplyTemplate } from "../../../api";
import { errorMessage, unwrap } from "../../../app/errors";
import { queryKey } from "../../../app/queryClient";

/** Chữ sau "/" khi ô gõ đang là lệnh chọn mẫu ("/xin" → "xin"); null khi không phải. */
export function slashQuery(text: string): string | null {
  const m = /^\/(\S{0,64})$/.exec(text);
  return m === null ? null : (m[1] ?? "");
}

export function useTemplates(q: string | null, pageId: string) {
  return useQuery({
    queryKey: queryKey("conversation.templates", pageId, q),
    queryFn: () => unwrap(listReplyTemplates({ query: { q: q ?? "", pageId } })),
    enabled: q !== null,
  });
}

export interface TemplateMenuProps {
  items: readonly ReplyTemplate[];
  active: number;
  pending: boolean;
  error: unknown;
  /** Tên Page của mẫu riêng một Page (mẫu chung: "mọi Page"). */
  pageName: string;
  onPick: (t: ReplyTemplate) => void;
}

export function TemplateMenu({ items, active, pending, error, pageName, onPick }: TemplateMenuProps) {
  return (
    <div className="cmp-slash">
      <div className="sugg" role="listbox" aria-label="Mẫu trả lời">
        {pending && <p className="sugg-i small muted">Đang tìm mẫu…</p>}
        {error !== null && (
          <p className="sugg-i small err-text" role="alert">
            {errorMessage(error)}
          </p>
        )}
        {!pending && error === null && items.length === 0 && (
          <p className="sugg-i small muted">Không có mẫu nào khớp. Quản lý tạo mẫu ở Cài đặt hộp chat.</p>
        )}
        {items.map((t, i) => (
          <div
            key={t.id}
            className="sugg-i"
            role="option"
            aria-selected={i === active}
            tabIndex={-1}
            onMouseDown={(e) => {
              e.preventDefault();
            }}
            onClick={() => {
              onPick(t);
            }}
          >
            <b>{`/${t.shortcut}`}</b>
            <span className="small muted">{t.pageId === undefined ? "mọi Page" : pageName}</span>
            <span className="sub ellip">{t.body}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
