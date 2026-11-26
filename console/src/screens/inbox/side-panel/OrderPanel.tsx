// Lên đơn và hoàn tất đơn nháp cạnh khung chat (Task 176; mockup khung-ben-phai.html trạng thái B, C; HV25, HV27, HV28;
// LV04 phía màn hình; 01 mục 6). Dùng lại nguyên OrderForm của M2:
// - Lên đơn: bối cảnh getConversationOrderContext → OrderForm với conversationId (máy chủ gắn đơn vào hội thoại, Page,
//   người bán của Page), khách của hội thoại, nguồn đơn tự điền.
// - Hoàn tất đơn nháp: đọc đơn (getOrder) → OrderForm chế độ sửa, confirmAfterSave: updateOrder (chỉ trường đổi,
//   version) rồi confirmOrder với version mới nhất; 409 version_conflict → "Có người vừa sửa, tải lại", không tự gửi lại.
// - Tin khách mới nhất trông như địa chỉ → nút "Lấy địa chỉ từ tin HH:MM của khách" trong form (HV26, Task 501).
// - Lưu xong → gợi ý một chạm "Gắn nhãn Đã lên đơn" (TagSuggestion).
// - Khung lên đơn đang mở → nhịp tim "drafting_order" mỗi 20 giây (hạn máy chủ 30 giây, Task 163).
// - Không có order.create ở kho (toàn shop hay theo kho) → khóa sẵn kèm lý do (HV28), máy chủ vẫn kiểm.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import {
  confirmOrder,
  getConversationOrderContext,
  setPresence,
  type ChatOrderRef,
  type Grant,
  type Order,
} from "../../../api";
import { ApiError, errorMessage, unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { queryKey } from "../../../app/queryClient";
import { useOrder } from "../../don/orderData";
import { latestMessagesQuery } from "../conversation/messageWrites";
import { OrderForm } from "../../len-don/OrderForm";
import { CONVERSATION_TOPIC } from "../useConversationList";
import { addressSuggestion, type AddressSuggestion } from "./addressRules";
import { OpenDrafts } from "./OpenDrafts";
import { canCreateOrder, canOnPage, cardKey, hasPermission } from "./sidePanelData";
import { TagSuggestion } from "./TagSuggestion";

/** Nhịp tim "đang lên đơn" (máy chủ giữ 30 giây). */
export const HEARTBEAT_MS = 20_000;
export const TEXT_DRAFT_CONFLICT = "Có người vừa sửa, tải lại đơn nháp để xem bản mới rồi hoàn tất.";

export interface OrderPanelProps {
  conversationId: string;
  pageId: string;
  /** Khách của hội thoại (chọn sẵn trong form). */
  customer: { id: string; code: string; fullName: string };
  drafts: readonly ChatOrderRef[];
  grants: readonly Grant[];
}

type Mode = { kind: "new" } | { kind: "draft"; id: string } | null;

/** Gợi ý địa chỉ từ trang tin mới nhất của hội thoại (bản đệm chung với dòng tin); chưa tải, lỗi, không có → undefined. */
function useAddressSuggestion(conversationId: string): AddressSuggestion | undefined {
  const q = useQuery(latestMessagesQuery(conversationId));
  const [openedAt] = useState(() => Date.now());
  return (q.data === undefined ? null : addressSuggestion(q.data.items, openedAt)) ?? undefined;
}

/** Gửi nhịp tim drafting_order khi active (mỗi lần một khóa mới, Q9); lỗi bỏ qua (chỉ là tín hiệu). */
function useDraftingHeartbeat(conversationId: string, active: boolean) {
  const beat = useWriteAction((key, id: string) =>
    unwrap(setPresence({ path: { id }, body: { state: "drafting_order" }, headers: { "Idempotency-Key": key } })),
  );
  // Lần gửi luôn dùng beat mới nhất mà không làm effect chạy lại mỗi lần vẽ (beat đổi khi trạng thái gửi đổi).
  const send = useRef((id: string) => beat.run(id));
  useEffect(() => {
    send.current = (id: string) => beat.run(id);
  });
  useEffect(() => {
    if (!active) return;
    void send.current(conversationId);
    const t = setInterval(() => {
      void send.current(conversationId);
    }, HEARTBEAT_MS);
    return () => {
      clearInterval(t);
    };
  }, [active, conversationId]);
}

function NewOrder({
  conversationId,
  customer,
  onCreated,
  address,
}: Pick<OrderPanelProps, "conversationId" | "customer"> & {
  onCreated: (o: Order) => void;
  address: AddressSuggestion | undefined;
}) {
  const ctx = useQuery({
    queryKey: queryKey(CONVERSATION_TOPIC, "order-context", conversationId),
    queryFn: () => unwrap(getConversationOrderContext({ path: { id: conversationId } })),
  });
  if (ctx.isError) {
    return (
      <div className="box box-err" role="alert">
        {errorMessage(ctx.error)}
      </div>
    );
  }
  if (ctx.data === undefined) return <p className="muted small">Đang mở khung lên đơn…</p>;
  const src = ctx.data.trafficSourceId;
  return (
    <OrderForm
      conversationId={ctx.data.conversationId}
      initialCustomer={customer}
      addressFromMessage={address}
      {...(src === undefined || src === null ? {} : { initialTrafficSourceId: src })}
      onCreated={onCreated}
    />
  );
}

/**
 * Hoàn tất một đơn nháp. Form dựng từ bản chụp đơn lúc mở (snap), không theo mỗi lần useOrder tải lại vì báo hiệu
 * realtime (dựng lại form làm mất phần đang gõ); "Tải lại đơn nháp" lấy bản mới làm bản chụp. updateOrder thành công mà
 * confirmOrder lỗi: bản chụp đổi sang đơn vừa lưu (version mới) để lần bấm sau không gặp version_conflict của chính mình.
 */
function CompleteDraft({
  id,
  onConfirmed,
  address,
}: {
  id: string;
  onConfirmed: (o: Order) => void;
  address: AddressSuggestion | undefined;
}) {
  const draft = useOrder(id);
  const [snap, setSnap] = useState<Order | null>(null);
  // Lần đầu có dữ liệu: chụp lại ngay trong lần vẽ (mẫu "điều chỉnh state khi dữ liệu đổi" của React).
  if (snap === null && draft.data !== undefined) setSnap(draft.data);
  const confirm = useWriteAction((key, b: { id: string; version: number }) =>
    unwrap(confirmOrder({ path: { id: b.id }, body: { version: b.version }, headers: { "Idempotency-Key": key } })),
  );
  const onSaved = async (o: Order) => {
    const r = await confirm.run({ id: o.id, version: o.version ?? 0 });
    if (r.ok) onConfirmed(r.data);
    else if (o !== snap) setSnap(o);
  };
  const reload = () => {
    confirm.reset();
    void draft.refetch().then((r) => {
      if (r.data !== undefined) setSnap(r.data);
    });
  };
  if (draft.isError && snap === null) {
    return (
      <div className="box box-err" role="alert">
        {errorMessage(draft.error)}
      </div>
    );
  }
  if (snap === null) return <p className="muted small">Đang mở đơn nháp…</p>;
  const conflict = confirm.error instanceof ApiError && confirm.error.code === "version_conflict";
  return (
    <>
      {conflict ? (
        <div className="box box-err" role="alert">
          <b>{TEXT_DRAFT_CONFLICT}</b>
          <div className="row" style={{ marginTop: 8 }}>
            <button type="button" className="btn btn-dark" onClick={reload}>
              Tải lại đơn nháp
            </button>
          </div>
        </div>
      ) : (
        confirm.message !== null && (
          <p className="err-text" role="alert">
            {confirm.message}
          </p>
        )
      )}
      <OrderForm
        key={`${snap.id}-${String(snap.version ?? 0)}`}
        addressFromMessage={address}
        edit={{
          order: snap,
          confirmAfterSave: true,
          onSaved: (o) => {
            void onSaved(o);
          },
          onReload: reload,
        }}
      />
    </>
  );
}

export function OrderPanel({ conversationId, pageId, customer, drafts, grants }: OrderPanelProps) {
  const qc = useQueryClient();
  const [mode, setMode] = useState<Mode>(null);
  const [saved, setSaved] = useState<Order | null>(null);
  const address = useAddressSuggestion(conversationId);
  const canCreate = canCreateOrder(grants);
  const canComplete = hasPermission(grants, "order.update") && hasPermission(grants, "order.confirm");
  const canTag = canOnPage(grants, "conversation.reply", pageId);
  useDraftingHeartbeat(conversationId, mode !== null);

  const done = (o: Order) => {
    setMode(null);
    setSaved(o);
    void qc.invalidateQueries({ queryKey: cardKey(conversationId) });
  };
  const draftCode = mode?.kind === "draft" ? drafts.find((d) => d.id === mode.id)?.code : undefined;

  return (
    <>
      <OpenDrafts
        drafts={drafts}
        canComplete={canComplete}
        onComplete={(d) => {
          setSaved(null);
          setMode({ kind: "draft", id: d.id });
        }}
      />
      {saved !== null && (
        <TagSuggestion
          conversationId={conversationId}
          order={saved}
          canTag={canTag}
          onClose={() => {
            setSaved(null);
          }}
        />
      )}
      <section className="sec" aria-label="Lên đơn">
        <div className="row-between">
          <h3>{draftCode === undefined ? "Lên đơn" : `Hoàn tất đơn ${draftCode}`}</h3>
          {mode !== null && (
            <button
              type="button"
              className="btn btn-text"
              onClick={() => {
                setMode(null);
              }}
            >
              Đóng
            </button>
          )}
        </div>
        {mode === null && !canCreate && (
          <>
            <div className="box box-info small">
              <b>Bạn chưa lên đơn được ở đây.</b> Đơn phải giữ hàng ở một kho, nên cần quyền &quot;Tạo đơn&quot; ở kho
              đó (hoặc toàn shop); quyền gán theo Page không đủ. Nhờ quản lý cấp ở Quản trị → Nhân viên.
            </div>
            <div className="act">
              <button type="button" className="btn btn-dark" disabled>
                Lưu và xác nhận
              </button>
              <span className="reason">Chưa có quyền tạo đơn ở kho giữ hàng.</span>
            </div>
            <p className="xs muted">Vẫn trả lời, gắn nhãn, ghi chú, nhắc việc được.</p>
          </>
        )}
        {mode === null && canCreate && (
          <div className="row">
            <button
              type="button"
              className="btn btn-dark"
              onClick={() => {
                setSaved(null);
                setMode({ kind: "new" });
              }}
            >
              Lên đơn
            </button>
            <span className="xs muted">Đơn tự gắn với hội thoại này và Page của nó.</span>
          </div>
        )}
        {mode?.kind === "new" && (
          <NewOrder conversationId={conversationId} customer={customer} onCreated={done} address={address} />
        )}
        {mode?.kind === "draft" && <CompleteDraft id={mode.id} onConfirmed={done} address={address} />}
      </section>
    </>
  );
}
