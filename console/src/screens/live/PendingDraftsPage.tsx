// Màn Đã chốt, thiếu địa chỉ (kế hoạch M4 Task 177; mockup docs/mockups/m4/thieu-dia-chi.html, HV49–HV51 kèm sửa
// HV49 ở "Kết luận duyệt"). listPendingDrafts của một phiên (Task 173), xếp hết hạn gần nhất trước; "Nhắc" từng dòng và
// "Nhắc tất cả" (remindPendingDrafts với mẫu xin địa chỉ của Page) hiện kết quả từng đơn kèm lý do máy chủ viết;
// đơn chưa nhắc được hiện lý do thật (canRemind.reason). Khách đã trả lời → "Mở hội thoại để hoàn tất" (tìm hội thoại
// của khách trên Page của phiên, tệp task 177 Quyết định 6).
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useNavigate } from "react-router";
import {
  getMe,
  getPageSettings,
  listCustomerConversations,
  remindPendingDrafts,
  type PendingDraft,
  type RemindOutcome,
} from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { hasPermission } from "../../app/nav";
import { queryKey } from "../../app/queryClient";
import { formatMoney } from "../products/productRules";
import { PENDING_KEY } from "./ClaimPanel";
import { askChip, hourMinute, itemsText, sessionTitle } from "./liveRules";
import { useNow, useSelectedSession } from "./LivePage";
import "../don/don.css";
import "./live.css";

/** Câu khi Page chưa chọn mẫu xin địa chỉ (remindPendingDrafts đòi templateId). */
export const TEXT_NO_TEMPLATE = "Page chưa chọn mẫu xin địa chỉ (Cài đặt hộp chat).";

interface RemindBody {
  sessionId: string;
  orderIds: string[] | null;
  templateId: string;
}

const byExpiry = (a: PendingDraft, b: PendingDraft) =>
  (a.expiresAt === undefined ? Infinity : Date.parse(a.expiresAt)) -
  (b.expiresAt === undefined ? Infinity : Date.parse(b.expiresAt));

/** "20:01 ngày mai", "còn 35 phút" (dưới 1 giờ, chữ đỏ), "20:01 24/11". */
function expiryText(iso: string | undefined, now: number): { text: string; soon: boolean } {
  if (iso === undefined) return { text: "—", soon: false };
  const t = Date.parse(iso);
  const left = t - now;
  if (left <= 0) return { text: "đã quá hạn", soon: true };
  if (left < 3_600_000) return { text: `còn ${String(Math.max(1, Math.floor(left / 60_000)))} phút`, soon: true };
  const day = (ms: number) => new Date(ms + 7 * 3_600_000).toISOString().slice(0, 10);
  if (day(t) === day(now)) return { text: hourMinute(iso), soon: false };
  if (day(t) === day(now + 86_400_000)) return { text: `${hourMinute(iso)} ngày mai`, soon: false };
  const d = day(t);
  return { text: `${hourMinute(iso)} ${d.slice(8, 10)}/${d.slice(5, 7)}`, soon: false };
}

function OpenConversation({ d, pageId }: { d: PendingDraft; pageId: string }) {
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const open = async () => {
    setBusy(true);
    setMsg(null);
    try {
      const page = await unwrap(listCustomerConversations({ path: { id: d.customer.id }, query: { limit: 100 } }));
      const onPage = page.items.filter((c) => c.pageId === pageId);
      const conv = onPage.find((c) => c.kind === "inbox") ?? onPage[0];
      if (conv === undefined) setMsg("Khách chưa có hội thoại trên Page này.");
      else void navigate(`/inbox/${conv.id}`);
    } catch {
      setMsg("Chưa mở được hội thoại. Thử lại sau ít giây.");
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="act" style={{ justifyItems: "end" }}>
      <button type="button" className="btn btn-dark" disabled={busy} onClick={() => void open()}>
        Mở hội thoại để hoàn tất
      </button>
      {msg !== null && <span className="reason">{msg}</span>}
    </div>
  );
}

export function PendingDraftsPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  const canOperate = hasPermission(me.data?.grants ?? [], "live.operate");
  const { sessions, list, s, drafts } = useSelectedSession();
  const now = useNow(60_000);
  const [repliedOnly, setRepliedOnly] = useState(false);
  const [outcomes, setOutcomes] = useState<{ all: boolean; results: RemindOutcome[] } | null>(null);
  const settings = useQuery({
    queryKey: queryKey("conversation.page-settings", s?.pageId),
    queryFn: () => unwrap(getPageSettings({ path: { id: s?.pageId ?? "" } })),
    enabled: s !== undefined,
  });
  const templateId = settings.data?.askAddressTemplateId;
  const write = useWriteAction((key, b: RemindBody) =>
    unwrap(
      remindPendingDrafts({
        path: { id: b.sessionId },
        body: { templateId: b.templateId, ...(b.orderIds === null ? {} : { orderIds: b.orderIds }) },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );

  const remind = async (orderIds: string[] | null) => {
    if (s === undefined || templateId === undefined) return;
    const r = await write.run({ sessionId: s.id, orderIds, templateId });
    if (!r.ok) return;
    setOutcomes({ all: orderIds === null, results: r.data.results });
    void qc.invalidateQueries({ queryKey: [PENDING_KEY] });
  };

  const items = [...(drafts.data?.items ?? [])].sort(byExpiry);
  const shown = repliedOnly ? items.filter((d) => d.customerRepliedAfterAsk) : items;
  const codeOf = (id: string) => items.find((d) => d.orderId === id);
  const rowOutcome = (id: string) =>
    outcomes?.all === false ? outcomes.results.find((o) => o.orderId === id) : undefined;
  const noRemind = !canOperate
    ? "Cần quyền chốt đơn từ comment Live."
    : templateId === undefined && settings.isSuccess
      ? TEXT_NO_TEMPLATE
      : null;

  const header = (
    <div className="page-h">
      <div>
        <p className="crumb">
          <Link to={s === undefined ? "/live" : `/live/${s.id}`}>Hộp chat · Live</Link> / Đã chốt, thiếu địa chỉ
        </p>
        <h1>Đã chốt, thiếu địa chỉ</h1>
        <p className="sub">
          Đơn nháp chốt từ comment live còn chờ khách gửi địa chỉ. Khách trả lời thì mở hội thoại để hoàn tất đơn ngay
          cạnh khung chat.
        </p>
      </div>
    </div>
  );

  if (sessions.isSuccess && s === undefined) {
    return (
      <div className="stack live-page">
        {header}
        <div className="empty">
          <h2>{list.length === 0 ? "Chưa có phiên live hôm nay" : "Không thấy phiên này trong các phiên hôm nay"}</h2>
          <Link className="btn btn-quiet" to="/live">
            Về Live
          </Link>
        </div>
      </div>
    );
  }

  const skipped = outcomes?.all === true ? outcomes.results.filter((o) => o.outcome === "skipped") : [];
  const queued = outcomes?.all === true ? outcomes.results.length - skipped.length : 0;

  return (
    <div className="stack live-page">
      {header}
      <div className="toolbar" style={{ margin: 0 }}>
        <span className="select-wrap" style={{ flex: "0 1 360px" }}>
          <select
            aria-label="Phiên"
            value={s?.id ?? ""}
            onChange={(e) => {
              setOutcomes(null);
              void navigate(`/live/${e.target.value}/thieu-dia-chi`);
            }}
          >
            {list.map((x) => (
              <option key={x.id} value={x.id}>
                {sessionTitle(x)}
              </option>
            ))}
          </select>
        </span>
        <button
          type="button"
          className="toggle"
          aria-pressed={repliedOnly}
          onClick={() => {
            setRepliedOnly(!repliedOnly);
          }}
        >
          Khách đã trả lời
        </button>
        <span className="grow" />
        {items.length > 0 && (
          <div className="act">
            <button
              type="button"
              className="btn btn-dark"
              disabled={noRemind !== null || write.pending || templateId === undefined}
              onClick={() => void remind(null)}
            >
              {`Nhắc tất cả (${String(items.length)})`}
            </button>
            <span className="reason">{noRemind ?? "Bỏ qua đơn vừa nhắc trong 60 phút."}</span>
          </div>
        )}
      </div>
      {drafts.isError && <div className="box box-err">Chưa tải được đơn nháp. Thử lại sau ít giây.</div>}
      {drafts.isPending && s !== undefined && <p className="small muted">Đang tải đơn nháp…</p>}
      {drafts.data !== undefined && (
        <div className="lstats">
          <span>
            Còn nháp <b>{items.length}</b>
          </span>
          <span>
            Đã xác nhận <b>{drafts.data.confirmedCount}</b>
          </span>
          <span>
            Hết hạn, đã hủy <b>{drafts.data.expiredCount}</b>
          </span>
        </div>
      )}
      {write.message !== null && <div className="box box-err small">{write.message}</div>}
      {outcomes?.all === true && (
        <div className="card">
          <div className="box box-ok small">
            <b className="ok-text">{`Đã xếp ${String(queued)} tin nhắc`}</b>
            {skipped.length > 0 && ` · bỏ qua ${String(skipped.length)}`}
          </div>
          {skipped.length > 0 && (
            <ul className="olines res" style={{ marginTop: 8 }}>
              {skipped.map((o) => {
                const d = codeOf(o.orderId);
                return (
                  <li key={o.orderId}>
                    <span>{d === undefined ? o.orderId : `${d.orderCode} · ${d.customer.name}`}</span>
                    <span className="small muted">{o.reason ?? "Bỏ qua"}</span>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}
      {drafts.data !== undefined && drafts.data.expiredCount > 0 && (
        <div className="box box-info small">
          <b>{`${String(drafts.data.expiredCount)} đơn nháp quá hạn đã tự hủy`}</b> vì khách không gửi địa chỉ kịp hạn.
          Hàng đã trả về "có thể bán"; lý do "Hết hạn đơn nháp" ghi ở từng đơn.{" "}
          <Link className="btn btn-text" to="/don">
            Xem đơn
          </Link>
        </div>
      )}
      {drafts.data !== undefined && items.length === 0 && (
        <div className="empty">
          <h2>Không còn đơn nào thiếu địa chỉ</h2>
          {s !== undefined && (
            <p className="small muted">
              {`Phiên "${sessionTitle(s)}": ${String(drafts.data.confirmedCount)} đơn đã xác nhận, ${String(drafts.data.expiredCount)} hết hạn.`}
            </p>
          )}
          <Link className="btn btn-quiet" to={s === undefined ? "/live" : `/live/${s.id}`}>
            Về Live
          </Link>
        </div>
      )}
      {shown.length > 0 && s !== undefined && (
        <div className="tbl-frame">
          <table className="tbl tdc">
            <thead>
              <tr>
                <th>Khách · đơn</th>
                <th>Món</th>
                <th className="r">Tổng</th>
                <th>Hết hạn</th>
                <th>Xin địa chỉ</th>
                <th>Khách trả lời</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {shown.map((d) => {
                const chip = askChip(d.ask);
                const exp = expiryText(d.expiresAt, now);
                const o = rowOutcome(d.orderId);
                const why = noRemind ?? (d.canRemind.allowed ? null : (d.canRemind.reason ?? "Chưa nhắc được"));
                return (
                  <tr key={d.orderId} className={chip.cls.includes("chip-err") ? "is-err" : undefined}>
                    <td data-label="Khách" className="lead">
                      <span className="stack-sm" style={{ gap: 0, minWidth: 0 }}>
                        <b className="ellip">{d.customer.name}</b>
                        <span className="xs muted num">{`${d.orderCode} · chốt ${hourMinute(d.claimedAt)} · ${d.claimedBy.name}`}</span>
                      </span>
                    </td>
                    <td data-label="Món">{itemsText(d.items)}</td>
                    <td data-label="Tổng" className="r">
                      <span className="money">{formatMoney(d.grandTotal)}</span>
                    </td>
                    <td data-label="Hết hạn" className="num">
                      {exp.soon ? <span className="err-text bold">{exp.text}</span> : exp.text}
                    </td>
                    <td data-label="Xin địa chỉ">
                      <span className="stack-sm" style={{ gap: 0 }}>
                        <span className={chip.cls}>{chip.label}</span>
                        {chip.reason !== null && <span className="xs">{chip.reason}</span>}
                      </span>
                    </td>
                    <td data-label="Khách trả lời">
                      {d.customerRepliedAfterAsk ? (
                        <span className="chip chip-ok">Đã trả lời</span>
                      ) : (
                        <span className="muted small">Chưa</span>
                      )}
                    </td>
                    <td className="c-act">
                      {d.customerRepliedAfterAsk ? (
                        <OpenConversation d={d} pageId={s.pageId} />
                      ) : (
                        <div className="act" style={{ justifyItems: "end" }}>
                          <button
                            type="button"
                            className="btn btn-quiet"
                            aria-label={`Nhắc ${d.orderCode}`}
                            disabled={why !== null || write.pending}
                            onClick={() => void remind([d.orderId])}
                          >
                            Nhắc
                          </button>
                          {o !== undefined ? (
                            <span className="reason">
                              {o.outcome === "queued" ? "Đã xếp tin nhắc" : (o.reason ?? "Bỏ qua")}
                            </span>
                          ) : (
                            why !== null && <span className="reason">{why}</span>
                          )}
                        </div>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
