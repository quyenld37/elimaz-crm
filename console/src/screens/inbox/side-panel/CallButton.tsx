// Gọi điện (Task 176; mockup khung-ben-phai.html trạng thái A "goi-dien", C; HV29, HV30): nút "Gọi <SĐT>" là liên kết
// tel: khi thẻ khách có SĐT (máy chủ chỉ trả SĐT cho người có customer.read); "Ghi kết quả cuộc gọi" mở hộp thoại (cần
// conversation.reply); dòng "Lần gọi trước" từ listCustomerCalls (cả nhóm khách, cuộc gọi trên Page được xem).
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { listCustomerCalls, type ConversationCustomerCard } from "../../../api";
import { unwrap } from "../../../app/errors";
import { displayPhone, shortDate } from "../../len-don/orderRules";
import { CallResultDialog } from "./CallResultDialog";
import { outcomeText } from "./labels";
import { callsKey, remindersKey } from "./sidePanelData";

export interface CallButtonProps {
  card: ConversationCustomerCard;
  conversationId: string;
  canReply: boolean;
}

export function CallButton({ card, conversationId, canReply }: CallButtonProps) {
  const qc = useQueryClient();
  const c = card.customer;
  const phone = c.phones?.[0];
  const shown = phone === undefined ? "" : displayPhone(phone);
  const [open, setOpen] = useState(false);
  const calls = useQuery({
    queryKey: callsKey(c.id),
    queryFn: () => unwrap(listCustomerCalls({ path: { id: c.id }, query: { limit: 1 } })),
  });
  const last = calls.data?.items[0];
  if (phone === undefined && !canReply && last === undefined) return null;
  return (
    <section className="sec" aria-label="Gọi điện">
      <h3>Gọi điện</h3>
      <div className="row">
        {phone !== undefined && (
          <a className="btn btn-line" href={`tel:${shown.replace(/\s/g, "")}`}>
            {`Gọi ${shown}`}
          </a>
        )}
        {canReply && (
          <button
            type="button"
            className="btn btn-quiet"
            onClick={() => {
              setOpen(true);
            }}
          >
            Ghi kết quả cuộc gọi
          </button>
        )}
      </div>
      {c.phones?.length === 0 && <p className="xs muted">Hồ sơ chưa có SĐT để gọi.</p>}
      {last !== undefined && (
        <p className="xs muted">
          {`Lần gọi trước: ${shortDate(last.createdAt)} · ${outcomeText(last.outcome)} · ${last.createdBy.name}`}
        </p>
      )}
      {open && (
        <CallResultDialog
          customerId={c.id}
          conversationId={conversationId}
          who={[c.name, shown].filter((x) => x !== "").join(" · ")}
          onClose={() => {
            setOpen(false);
          }}
          onSaved={() => {
            setOpen(false);
            void qc.invalidateQueries({ queryKey: callsKey(c.id) });
            void qc.invalidateQueries({ queryKey: remindersKey() });
          }}
        />
      )}
    </section>
  );
}
