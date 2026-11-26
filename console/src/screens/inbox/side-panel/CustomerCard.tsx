// Thẻ khách của khung bên phải (Task 176; mockup docs/mockups/m4/khung-ben-phai.html trạng thái A, B, C; HV24, HV29;
// DH03 phía màn hình). Số của M2 lấy nguyên từ getConversationCustomerCard (không tính lại): Hoàn = returnRateBp làm
// tròn xuống; Nhận hàng = phần còn lại của cùng mẫu số (giao / (giao + hoàn)). Mã khách, nhãn khách ("Hay hoàn" tô đỏ ô
// Hoàn) đọc từ hồ sơ (getCustomer, chỉ khi có customer.read); SĐT chỉ có khi máy chủ trả (customer.read). Cờ "không
// liên hệ" bật/tắt bằng setCustomerDoNotContact khi có customer.write.
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { setCustomerDoNotContact, type ConversationCustomerCard, type Customer } from "../../../api";
import { unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { STATUS_TEXT } from "../../don/labels";
import { tagChip } from "../../khach/customerRules";
import { displayPhone, shortDate } from "../../len-don/orderRules";
import { formatMoney } from "../../products/productRules";
import { floorPercent } from "./labels";
import { cardKey } from "./sidePanelData";

/** Nhãn khách của M2 làm ô Hoàn tô đỏ (HV24). */
const RETURNS_TAG = "Hay hoàn";

export interface CustomerCardProps {
  card: ConversationCustomerCard;
  /** Hồ sơ khách (getCustomer) khi người xem có customer.read: mã, nhãn khách. */
  profile?: Customer | undefined;
  canWriteCustomer: boolean;
  /** Hội thoại của thẻ (tải lại thẻ sau khi đổi cờ); không có thì không tải lại. */
  conversationId?: string;
  /** Khách tiềm năng mới (CH08, ConversationSummary.newLead, Task 501): chip trước nhãn khách. */
  newLead?: boolean;
}

const STATUS_BY_KEY: ReadonlyMap<string, string> = new Map(Object.entries(STATUS_TEXT));
const statusText = (s: string) => STATUS_BY_KEY.get(s) ?? s;

/** "n · p%" khi khách đã có đơn giao xong hoặc hoàn (mẫu số của returnRateBp), không thì chỉ "n". */
const countRate = (n: number, bp: number, done: boolean) =>
  done ? `${String(n)} · ${String(floorPercent(bp))}%` : String(n);

function Stat({ name, value, bad = false }: { name: string; value: string; bad?: boolean }) {
  return (
    <div className={bad ? "stat is-bad" : "stat"} role="group" aria-label={name}>
      <span className="xs muted">{name}</span>
      <span className="v">{value}</span>
    </div>
  );
}

export function CustomerCard({ card, profile, canWriteCustomer, conversationId, newLead = false }: CustomerCardProps) {
  const qc = useQueryClient();
  const c = card.customer;
  const phone = c.phones?.[0];
  const done = c.deliveredCount + c.returnedCount;
  const hasOrders = c.orderCount > 0;
  const tags = profile?.tags ?? [];
  const badReturns = tags.some((t) => t.tag.name === RETURNS_TAG);
  const dnc = useWriteAction((key, on: boolean) =>
    unwrap(setCustomerDoNotContact({ path: { id: c.id }, body: { on }, headers: { "Idempotency-Key": key } })),
  );
  const toggle = async (on: boolean) => {
    const r = await dnc.run(on);
    if (!r.ok || conversationId === undefined) return;
    void qc.invalidateQueries({ queryKey: cardKey(conversationId) });
  };
  const sub = [profile?.code, phone === undefined ? "chưa có SĐT" : displayPhone(phone)].filter(
    (x): x is string => x !== undefined && x !== "",
  );

  return (
    <>
      <div className="sec">
        <div className="row" style={{ flexWrap: "nowrap" }}>
          <span className="avatar" aria-hidden="true">
            {c.name.trim().charAt(0).toUpperCase()}
          </span>
          <div className="grow">
            <b className="ellip" style={{ display: "block" }}>
              {c.name}
            </b>
            <span className="small muted num">{sub.join(" · ")}</span>
          </div>
          {profile !== undefined && (
            <Link className="btn btn-quiet" to={`/khach/${c.rootId}`}>
              Hồ sơ
            </Link>
          )}
        </div>
        {(tags.length > 0 || newLead) && (
          <div className="lbs">
            {newLead && <span className="chip">Khách tiềm năng mới</span>}
            {tags.map((t) => (
              <span key={t.tag.id} className={tagChip(t.tag.color)}>
                {t.tag.name}
              </span>
            ))}
          </div>
        )}
        {hasOrders ? (
          <div className="stats">
            <Stat name="Số đơn" value={String(c.orderCount)} />
            <Stat name="Tổng chi" value={formatMoney(c.totalSpent)} />
            <Stat name="Nhận hàng" value={countRate(c.deliveredCount, 10000 - c.returnRateBp, done > 0)} />
            <Stat name="Hoàn" value={countRate(c.returnedCount, c.returnRateBp, done > 0)} bad={badReturns} />
          </div>
        ) : (
          <p className="small muted">Khách mới, chưa có đơn</p>
        )}
        {c.doNotContact ? (
          <div className="box box-err small">
            <b>Không liên hệ.</b> Khách yêu cầu không gọi, không nhắn chào hàng. CRM không gửi tin tự động (tin chào, tự
            trả lời, tự nhắn lại) và không đưa khách vào danh sách nhắc lại. Nhân viên vẫn nhắn và gọi được.
          </div>
        ) : null}
        {canWriteCustomer && (
          <div className="row">
            <button
              type="button"
              className="btn btn-quiet"
              disabled={dnc.pending}
              onClick={() => {
                void toggle(!c.doNotContact);
              }}
            >
              {c.doNotContact ? "Bỏ cờ không liên hệ" : "Đặt không liên hệ"}
            </button>
          </div>
        )}
        {dnc.message !== null && (
          <p className="err-text" role="alert">
            {dnc.message}
          </p>
        )}
      </div>
      {card.mergeSuggestionCustomerId !== undefined && (
        <div className="sec">
          <div className="box box-soft small">
            <b>Có thể là cùng một khách.</b> Một hồ sơ khác có cùng SĐT hoặc thông tin trùng. Không tự gộp.
            <div className="row" style={{ marginTop: 8 }}>
              <Link className="btn btn-line" to={`/khach/${c.rootId}/gop?voi=${card.mergeSuggestionCustomerId}`}>
                Xem hai hồ sơ
              </Link>
            </div>
          </div>
        </div>
      )}
      {card.recentOrders.length > 0 && (
        <div className="sec">
          <h3>Đơn gần đây</h3>
          <ul className="olines" aria-label="Đơn gần đây">
            {card.recentOrders.map((o) => (
              <li key={o.id}>
                <Link to={`/don/${o.id}`}>{`${o.code} · ${shortDate(o.createdAt)} · ${statusText(o.status)}`}</Link>
                <span className="money">{formatMoney(o.grandTotal)}</span>
              </li>
            ))}
          </ul>
          {profile !== undefined && c.orderCount > card.recentOrders.length && (
            <Link className="btn btn-text" to={`/khach/${c.rootId}`}>
              {`Xem cả ${String(c.orderCount)} đơn`}
            </Link>
          )}
        </div>
      )}
    </>
  );
}
