// Lịch sử sửa của comment (Task 175; mockup hoi-thoai.html trạng thái C; CH13; HV21 sửa: thu gọn mặc định, bấm "Xem bản
// trước" mới tải listMessageRevisions và mở). Nội dung khách viết hiện dạng chữ.
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { listMessageRevisions, type ChatMessage } from "../../../api";
import { errorMessage, unwrap } from "../../../app/errors";
import { queryKey } from "../../../app/queryClient";
import { shortTime } from "../../../app/time";

const SOURCE: Readonly<Record<string, string>> = {
  customer: "bản của khách",
  staff: "nhân viên",
  page_external: "ngoài CRM",
  automation: "tự động",
};

export function RevisionsPopover({ m }: { m: ChatMessage }) {
  const [open, setOpen] = useState(false);
  const q = useQuery({
    queryKey: queryKey("conversation.revisions", m.id, m.editedCount),
    queryFn: () => unwrap(listMessageRevisions({ path: { id: m.id } })),
    enabled: open,
  });
  // Dòng ẩn, hiện, xóa giữ nguyên nội dung (Task 154): chỉ hiện bản có chữ khác bản sau nó.
  const items = (q.data?.items ?? []).filter((r) => r.body !== undefined && r.body !== m.body);
  return (
    <>
      <button
        type="button"
        className="btn btn-text"
        aria-expanded={open}
        onClick={() => {
          setOpen(!open);
        }}
      >
        {open ? "Ẩn bản trước" : "Xem bản trước"}
      </button>
      {open && q.isPending && <span className="small muted">Đang tải lịch sử sửa…</span>}
      {open && q.isError && (
        <span className="small err-text" role="alert">
          {errorMessage(q.error)}
        </span>
      )}
      {open && q.isSuccess && (
        <ul className="revs" aria-label="Lịch sử sửa">
          <li>
            <span className="at">bản hiện tại</span>
            {m.body ?? ""}
          </li>
          {items.map((r, i) => (
            <li key={`${r.recordedAt}-${String(i)}`}>
              <span className="at">{`${shortTime(r.recordedAt)} · ${SOURCE[r.source] ?? r.source}`}</span>
              {r.body ?? ""}
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
