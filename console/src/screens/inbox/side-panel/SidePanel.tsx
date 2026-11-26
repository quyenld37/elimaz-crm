// Khung bên phải của /inbox/:conversationId (kế hoạch M4 Task 36, số toàn cục 176; mockup
// docs/mockups/m4/khung-ben-phai.html trạng thái A–C; HV24–HV31). Thứ tự theo mockup: thẻ khách (chỉ số M2, gợi ý gộp,
// đơn gần đây, không liên hệ), đơn nháp đang mở và lên đơn, nhãn, người phụ trách, nhắc việc, gọi điện. Quyền hộp chat
// xét theo Page của hội thoại (canOnPage); quyền khách, đơn xét toàn shop như máy chủ. Lúc đang tải, lỗi: không vẽ như
// khách rỗng. Điện thoại (HV02): khung nằm dưới hội thoại, nút "‹ Hội thoại" về đầu hội thoại (#hoi-thoai, Task 501).
import { useQuery } from "@tanstack/react-query";
import { getCustomer } from "../../../api";
import { errorMessage, unwrap } from "../../../app/errors";
import { queryKey } from "../../../app/queryClient";
import { CUSTOMER_TOPIC } from "../../len-don/CustomerPicker";
import { AssigneeControl } from "./AssigneeControl";
import { CallButton } from "./CallButton";
import { CustomerCard } from "./CustomerCard";
import { OrderPanel } from "./OrderPanel";
import { ReminderForm } from "./ReminderForm";
import { canOnPage, hasPermission, useCard, useDetail, useMe } from "./sidePanelData";
import { TagPicker } from "./TagPicker";
import "./side-panel.css";

export interface SidePanelProps {
  conversationId: string;
}

export default function SidePanel({ conversationId }: SidePanelProps) {
  const me = useMe();
  const detail = useDetail(conversationId);
  const card = useCard(conversationId);
  const grants = me.data?.grants ?? [];
  const canReadCustomer = hasPermission(grants, "customer.read");
  const customerId = card.data?.customer.id ?? "";
  const profile = useQuery({
    queryKey: queryKey(CUSTOMER_TOPIC, "detail", customerId),
    enabled: canReadCustomer && customerId !== "",
    queryFn: () => unwrap(getCustomer({ path: { id: customerId } })),
  });

  const failed = [me, detail, card].find((q) => q.isError);
  if (failed !== undefined) {
    return (
      <div className="side">
        <div className="sec">
          <div className="box box-err" role="alert">
            {errorMessage(failed.error)}
          </div>
        </div>
      </div>
    );
  }
  if (me.data === undefined || detail.data === undefined || card.data === undefined) {
    return (
      <div className="side">
        <p className="sec muted small">Đang tải thông tin khách…</p>
      </div>
    );
  }
  const d = detail.data;
  const c = card.data;
  const pageId = d.page.id;
  const canReply = canOnPage(grants, "conversation.reply", pageId);
  const p = profile.data;
  return (
    <div className="side">
      <a className="btn btn-quiet only-phone" href="#hoi-thoai" style={{ margin: "8px 0" }}>
        ‹ Hội thoại
      </a>
      <section aria-label="Khách">
        <CustomerCard
          card={c}
          newLead={d.conversation.newLead === true}
          profile={p}
          canWriteCustomer={hasPermission(grants, "customer.write")}
          conversationId={conversationId}
        />
      </section>
      <OrderPanel
        conversationId={conversationId}
        pageId={pageId}
        customer={{ id: c.customer.id, code: p?.code ?? "", fullName: c.customer.name }}
        drafts={c.openDrafts}
        grants={grants}
      />
      <TagPicker conversationId={conversationId} tags={d.conversation.tags} canEdit={canReply} />
      <AssigneeControl detail={d} grants={grants} meId={me.data.userId} />
      <ReminderForm conversationId={conversationId} canEdit={canReply} />
      <CallButton card={c} conversationId={conversationId} canReply={canReply} />
    </div>
  );
}
