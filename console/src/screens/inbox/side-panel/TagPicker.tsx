// Nhãn của hội thoại (Task 176; mockup khung-ben-phai.html trạng thái A "nhan"; CH07 phía màn hình): nhãn đang gắn (màu
// theo tagClass của Inbox) có nút gỡ "×" (removeConversationTag); "+ Nhãn" mở danh sách nhãn chưa gắn, chọn → gắn
// (addConversationTag). Gắn, gỡ cần conversation.reply trên Page (máy chủ kiểm); người chỉ xem thấy nhãn, không có nút.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { addConversationTag, listConversationTags, removeConversationTag, type ConversationTag } from "../../../api";
import { errorMessage, unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { tagClass } from "../inboxRules";
import { detailKey, tagsKey } from "./sidePanelData";

export interface TagPickerProps {
  conversationId: string;
  tags: readonly ConversationTag[];
  canEdit: boolean;
}

interface TagChange {
  tagId: string;
  on: boolean;
}

export function TagPicker({ conversationId, tags, canEdit }: TagPickerProps) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const all = useQuery({
    queryKey: tagsKey(),
    enabled: canEdit && open,
    queryFn: () => unwrap(listConversationTags()),
  });
  const write = useWriteAction((key, c: TagChange) => {
    const opts = { path: { id: conversationId, tagId: c.tagId }, headers: { "Idempotency-Key": key } };
    return unwrap(c.on ? addConversationTag(opts) : removeConversationTag(opts));
  });
  const change = async (c: TagChange) => {
    const r = await write.run(c);
    if (!r.ok) return;
    setOpen(false);
    void qc.invalidateQueries({ queryKey: detailKey(conversationId) });
  };
  const has = new Set(tags.map((t) => t.id));
  const free = (all.data?.items ?? []).filter((t) => !has.has(t.id)).sort((a, b) => a.sort - b.sort);

  return (
    <section className="sec" aria-label="Nhãn">
      <div className="row-between">
        <h3>Nhãn</h3>
        {canEdit && (
          <button
            type="button"
            className="btn btn-quiet"
            aria-expanded={open}
            onClick={() => {
              setOpen(!open);
            }}
          >
            + Nhãn
          </button>
        )}
      </div>
      {tags.length === 0 ? (
        <p className="small muted">Chưa gắn nhãn.</p>
      ) : (
        <div className="lbs">
          {tags.map((t) => (
            <span key={t.id} className={canEdit ? `${tagClass(t.color)} lb-x` : tagClass(t.color)}>
              {t.name}
              {canEdit && (
                <button
                  type="button"
                  className="ibtn"
                  aria-label={`Gỡ nhãn ${t.name}`}
                  disabled={write.pending}
                  onClick={() => {
                    void change({ tagId: t.id, on: false });
                  }}
                >
                  ×
                </button>
              )}
            </span>
          ))}
        </div>
      )}
      {open && (
        <>
          {all.isError && (
            <p className="err-text" role="alert">
              {errorMessage(all.error)}
            </p>
          )}
          {all.isSuccess && free.length === 0 && <p className="small muted">Đã gắn mọi nhãn của shop.</p>}
          {free.length > 0 && (
            <ul className="pick" role="listbox" aria-label="Chọn nhãn">
              {free.map((t) => (
                <li
                  key={t.id}
                  role="option"
                  aria-selected={false}
                  aria-label={t.name}
                  className={tagClass(t.color)}
                  tabIndex={0}
                  onClick={() => {
                    if (!write.pending) void change({ tagId: t.id, on: true });
                  }}
                  onKeyDown={(e) => {
                    if ((e.key === "Enter" || e.key === " ") && !write.pending) {
                      e.preventDefault();
                      void change({ tagId: t.id, on: true });
                    }
                  }}
                >
                  {t.name}
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      {write.message !== null && (
        <p className="err-text" role="alert">
          {write.message}
        </p>
      )}
    </section>
  );
}
