// Dòng thời gian của cả nhóm khách và ô thêm ghi chú (mockup khach-ho-so.html trạng thái A, B). Dòng thời gian đọc
// getCustomerTimeline (đơn, đổi trạng thái đơn, ghi chú, gộp, hoàn tác gộp, nhãn; mới nhất trước, "Xem cũ hơn" theo con
// trỏ); summary không có SĐT, địa chỉ. Ghi chú chỉ thêm, không sửa, không xóa (addCustomerNote, cần customer.write).
// M4 (C7, Task 176): mục conversation (tên Page, liên kết mở /inbox/:id) và call (kết quả cuộc gọi, người gọi); máy chủ
// không trả nội dung tin, ghi chú cuộc gọi. Tên Page đọc listChannelAccounts (chỉ khi có mục hội thoại; đọc lỗi thì bỏ
// tên Page, vẫn hiện mục).
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { Link } from "react-router";
import { addCustomerNote, listChannelAccounts, type Customer, type OrderStatus, type TimelineItem } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { shortTime } from "../../app/time";
import { STATUS_TEXT } from "../don/labels";
import { conversationKindText, outcomeText } from "../inbox/side-panel/labels";
import { formatMoney } from "../products/productRules";
import { headers, reloadCustomers, timelineKey, useTimeline } from "./customerData";

const MAX_NOTE = 4000;
const timeText = (iso: string) => shortTime(iso).replace(" ", " · ");
const isStatus = (s: string | undefined): s is OrderStatus => s !== undefined && Object.hasOwn(STATUS_TEXT, s);

function mergeText(i: TimelineItem, c: Customer): string {
  const survivor = i.summary?.survivor_id ?? "";
  if (survivor === c.rootId) return "Gộp một hồ sơ vào hồ sơ này";
  if (c.memberIds.includes(survivor)) return "Gộp một hồ sơ vào một hồ sơ trong nhóm";
  return "Gộp hồ sơ vào một khách khác";
}

/** Chủ đề realtime của tài khoản kênh (máy chủ: channel.TopicStatus; cùng khóa với màn Hộp chat). */
const CHANNEL_TOPIC = "channel.status";

/** Nội dung một mục; null khi loại mục màn chưa biết. pages: tên Page theo mã (mục hội thoại, C7). */
function body(i: TimelineItem, c: Customer, pages: ReadonlyMap<string, string>): ReactNode {
  const s = i.summary ?? {};
  switch (i.kind) {
    case "order_created": {
      const total = Number(s.grand_total);
      return (
        <b>
          Đơn mới{" "}
          <Link className="cell-a num" to={`/don/${i.refId}`}>
            {s.code ?? ""}
          </Link>
          {Number.isSafeInteger(total) && (
            <>
              {" "}
              · <span className="money">{formatMoney(total)}</span>
            </>
          )}
        </b>
      );
    }
    case "order_status":
      return (
        <b>
          Đơn{" "}
          {s.order_id === undefined ? (
            (s.code ?? "")
          ) : (
            <Link className="cell-a num" to={`/don/${s.order_id}`}>
              {s.code ?? ""}
            </Link>
          )}
          {` → ${isStatus(s.to_status) ? STATUS_TEXT[s.to_status] : (s.to_status ?? "")}`}
        </b>
      );
    case "note":
      return <b>Ghi chú</b>;
    case "merge":
      return <b>{mergeText(i, c)}</b>;
    case "merge_undone":
      return <b>Hoàn tác một lần gộp</b>;
    case "tag":
      return <b>{`Gắn nhãn "${s.name ?? ""}" (${s.source === "rule" ? "theo quy tắc" : "gắn tay"})`}</b>;
    case "conversation": {
      const page = pages.get(s.pageId ?? "");
      return (
        <>
          <b>{conversationKindText(s.kind) + (page === undefined ? "" : ` · ${page}`)}</b>{" "}
          <Link className="cell-a" to={`/inbox/${s.conversationId ?? i.refId}`}>
            Mở hội thoại
          </Link>
        </>
      );
    }
    case "call":
      return <b>{`Cuộc gọi · ${outcomeText(s.outcome ?? "")}`}</b>;
    default:
      return null;
  }
}

export function CustomerTimeline({ customer }: { customer: Customer }) {
  const tl = useTimeline(customer.rootId);
  const all = tl.data?.pages.flatMap((p) => p.items) ?? [];
  const hasConversation = all.some((i) => i.kind === "conversation");
  const accounts = useQuery({
    queryKey: queryKey(CHANNEL_TOPIC, "accounts"),
    enabled: hasConversation,
    queryFn: () => unwrap(listChannelAccounts()),
  });
  const pages = new Map((accounts.data?.items ?? []).map((a) => [a.id, a.name]));
  const items = all.filter((i) => body(i, customer, pages) !== null);
  const n = customer.memberIds.length;
  return (
    <div className="card">
      <div className="row-between">
        <h2>Dòng thời gian</h2>
        {n > 1 && <span className="small muted">{`Của cả ${String(n)} hồ sơ`}</span>}
      </div>
      {tl.isError && <div className="box box-err">{errorMessage(tl.error)}</div>}
      {tl.isSuccess && items.length === 0 && (
        <div className="empty" style={{ marginTop: 12 }}>
          <p className="muted">Chưa có hoạt động.</p>
        </div>
      )}
      {items.length > 0 && (
        <ul className="tl">
          {items.map((i) => (
            <li key={`${i.kind}-${i.refId}`}>
              <span className="at">{timeText(i.at)}</span>
              <div>
                {body(i, customer, pages)}
                {i.actor !== undefined && (
                  <>
                    {" "}
                    · <span className="who">{i.actor.name}</span>
                  </>
                )}
                {i.kind === "note" && i.note !== undefined && (
                  <div className="note-box" style={{ marginTop: 4 }}>
                    {i.note}
                  </div>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {items.length > 0 && (
        <div className="more">
          <span>Mới nhất trước</span>
          {tl.hasNextPage && (
            <button
              type="button"
              className="btn btn-line"
              disabled={tl.isFetchingNextPage}
              onClick={() => {
                void tl.fetchNextPage();
              }}
            >
              Xem cũ hơn
            </button>
          )}
        </div>
      )}
    </div>
  );
}

export function NotesPanel({ customerId }: { customerId: string }) {
  const qc = useQueryClient();
  const [text, setText] = useState("");
  const add = useWriteAction((key, b: { id: string; body: string }) =>
    unwrap(addCustomerNote({ path: { id: b.id }, body: { body: b.body }, headers: headers(key) })),
  );
  const save = async () => {
    const t = text.trim();
    if (t === "") return;
    const r = await add.run({ id: customerId, body: t });
    if (!r.ok) return;
    setText("");
    void qc.invalidateQueries({ queryKey: timelineKey(customerId) });
    reloadCustomers(qc);
  };
  return (
    <div className="card">
      <h2>Thêm ghi chú</h2>
      <label className="field">
        <span className="sr">Ghi chú</span>
        <textarea
          className="input"
          placeholder="Chỉ người trong shop thấy; không sửa, không xóa được sau khi lưu"
          maxLength={MAX_NOTE}
          style={{ minHeight: 72 }}
          value={text}
          onChange={(e) => {
            setText(e.target.value);
          }}
        />
      </label>
      {add.message !== null && (
        <p className="err-text" role="alert">
          {add.message}
        </p>
      )}
      <div className="row" style={{ justifyContent: "flex-end", marginTop: 8 }}>
        <button
          type="button"
          className="btn btn-line"
          disabled={add.pending || text.trim() === ""}
          onClick={() => {
            void save();
          }}
        >
          Lưu ghi chú
        </button>
      </div>
    </div>
  );
}
