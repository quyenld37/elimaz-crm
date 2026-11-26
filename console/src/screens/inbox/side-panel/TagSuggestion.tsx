// Gợi ý một chạm sau khi lưu đơn cạnh khung chat (Task 176; mockup khung-ben-phai.html trạng thái B "goi-y-nhan"; HV27,
// Q25): hộp "Đã lưu DH… · trạng thái", nút 'Gắn nhãn "Đã lên đơn"' (addConversationTag, không tự gắn) và "Bỏ qua".
// Nút chỉ có khi shop có nhãn tên "Đã lên đơn" và người xem có conversation.reply trên Page.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { addConversationTag, listConversationTags, type Order } from "../../../api";
import { unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { STATUS_TEXT } from "../../don/labels";
import { formatMoney } from "../../products/productRules";
import { detailKey, tagsKey } from "./sidePanelData";

/** Tên nhãn gợi ý (Q25). */
export const ORDERED_TAG = "Đã lên đơn";

export interface TagSuggestionProps {
  conversationId: string;
  order: Order;
  canTag: boolean;
  onClose: () => void;
}

export function TagSuggestion({ conversationId, order, canTag, onClose }: TagSuggestionProps) {
  const qc = useQueryClient();
  const tags = useQuery({ queryKey: tagsKey(), enabled: canTag, queryFn: () => unwrap(listConversationTags()) });
  const tag = (tags.data?.items ?? []).find((t) => t.name === ORDERED_TAG);
  const add = useWriteAction((key, tagId: string) =>
    unwrap(addConversationTag({ path: { id: conversationId, tagId }, headers: { "Idempotency-Key": key } })),
  );
  const apply = async (tagId: string) => {
    const r = await add.run(tagId);
    if (!r.ok) return;
    void qc.invalidateQueries({ queryKey: detailKey(conversationId) });
    onClose();
  };
  return (
    <div className="sec">
      <div className="box box-ok small">
        <b className="ok-text">{`Đã lưu ${order.code} · ${STATUS_TEXT[order.status]}`}</b> ·{" "}
        <span className="money">{formatMoney(order.grandTotal)}</span>
      </div>
      <div className="row">
        {canTag && tag !== undefined && (
          <button
            type="button"
            className="btn btn-line"
            disabled={add.pending}
            onClick={() => {
              void apply(tag.id);
            }}
          >
            {`Gắn nhãn "${ORDERED_TAG}"`}
          </button>
        )}
        <button type="button" className="btn btn-text" onClick={onClose}>
          Bỏ qua
        </button>
      </div>
      {add.message !== null && (
        <p className="err-text" role="alert">
          {add.message}
        </p>
      )}
      <p className="xs muted">Đơn hiện trong &quot;Đơn gần đây&quot; của khách.</p>
    </div>
  );
}
