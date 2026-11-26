// Khung chốt một comment live (mockup docs/mockups/m4/live.html trạng thái A khung bên phải, trạng thái B; HV42–HV45).
// getLiveCommentContext (Task 172): khách rút gọn, SĐT tách từ comment. Tìm hàng bằng VariantPicker của M2 (dùng lại;
// searchVariants không gửi sellerId — người bán lọc ở tầng dưới, máy chủ kiểm lại người bán của Page khi chốt).
// claimLiveComment qua useWriteAction (khóa sinh khi bấm, tự gửi lại cùng khóa khi bận). Lỗi: comment_claimed,
// out_of_stock, open_draft_exists (hai nút Thêm vào đơn / Tạo đơn mới), seller_mismatch, version_conflict, 403.
// Mở khung → nhịp tim drafting_order mỗi 10 s (khóa mới mỗi nhịp) và đọc ai khác đang ở hội thoại đó.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import {
  claimLiveComment,
  getLiveCommentContext,
  getMe,
  getPageSettings,
  getPresence,
  listReplyTemplates,
  setPresence,
  type ClaimLiveCommentRequest,
  type ClaimLiveCommentResult,
  type LiveComment,
  type LiveDraftRef,
  type PresenceState,
  type VariantSearchHit,
} from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { displayPhone } from "../len-don/orderRules";
import { VariantPicker, optionText } from "../len-don/VariantPicker";
import { formatMoney } from "../products/productRules";
import { askAfterClaim, claimProblem, clockTime, itemsText, type ClaimProblem } from "./liveRules";

/** Nhịp tim "đang lên đơn" (hạn 30 s ở máy chủ). */
export const PRESENCE_EVERY_MS = 10_000;
/** Số lượng tối đa mỗi món của lệnh chốt (hợp đồng ClaimLiveCommentItem). */
const MAX_QTY = 20;

export const PENDING_KEY = "live.pending-drafts";

const PRESENCE_TEXT: Record<PresenceState, string> = {
  viewing: "cũng đang xem",
  typing: "đang trả lời",
  drafting_order: "đang lên đơn",
};

interface Line {
  hit: VariantSearchHit;
  qty: number;
}

interface ClaimBody {
  commentId: string;
  req: ClaimLiveCommentRequest;
}

export interface ClaimPanelProps {
  comment: LiveComment;
  pageId: string;
  canCreate: boolean;
  canUpdate: boolean;
  canReadStock: boolean;
  /** Chốt xong: dòng comment đổi trạng thái ngay (không chờ báo hiệu). */
  onClaimed: (messageId: string, r: ClaimLiveCommentResult) => void;
  onClose: () => void;
}

const lineName = (h: VariantSearchHit) => [h.productName, optionText(h)].filter(Boolean).join(" ");

/** Nhịp tim drafting_order của mình và danh sách người khác đang ở hội thoại (getPresence). */
function usePresence(conversationId: string) {
  useEffect(() => {
    const beat = () => {
      void unwrap(
        setPresence({
          path: { id: conversationId },
          body: { state: "drafting_order" },
          headers: { "Idempotency-Key": crypto.randomUUID() },
        }),
      ).catch(() => undefined);
    };
    beat();
    const t = setInterval(beat, PRESENCE_EVERY_MS);
    return () => {
      clearInterval(t);
    };
  }, [conversationId]);
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  const others = useQuery({
    queryKey: queryKey("conversation.presence", conversationId),
    queryFn: () => unwrap(getPresence({ path: { id: conversationId } })),
    refetchInterval: PRESENCE_EVERY_MS,
  });
  return (others.data?.items ?? []).filter((p) => p.userId !== me.data?.userId);
}

/** Tên mẫu xin địa chỉ của Page (cài đặt Page); null khi Page chưa chọn mẫu. */
function useAskTemplate(pageId: string): { loading: boolean; title: string | null } {
  const settings = useQuery({
    queryKey: queryKey("conversation.page-settings", pageId),
    queryFn: () => unwrap(getPageSettings({ path: { id: pageId } })),
  });
  const tid = settings.data?.askAddressTemplateId;
  const templates = useQuery({
    queryKey: queryKey("conversation.reply-templates", pageId),
    queryFn: () => unwrap(listReplyTemplates({ query: { pageId } })),
    enabled: tid !== undefined,
  });
  if (settings.isPending) return { loading: true, title: null };
  if (tid === undefined) return { loading: false, title: null };
  return { loading: false, title: templates.data?.items.find((t) => t.id === tid)?.title ?? "" };
}

function Problem({
  p,
  message,
  lines,
  canUpdate,
  pending,
  onAdd,
  onNew,
  onFix,
  onClose,
}: {
  p: ClaimProblem | null;
  message: string | null;
  lines: readonly Line[];
  canUpdate: boolean;
  pending: boolean;
  onAdd: (d: LiveDraftRef) => void;
  onNew: () => void;
  onFix: () => void;
  onClose: () => void;
}) {
  if (p === null) return message === null ? null : <div className="box box-err small">{message}</div>;
  switch (p.kind) {
    case "claimed":
      return (
        <div className="stack-sm">
          <div className="box box-info small">
            <b>{`${p.by} đang lên đơn cho comment này.`}</b> Chỉ một đơn cho mỗi comment.
          </div>
          <div className="row">
            {p.orderId !== null && (
              <Link className="btn btn-line" to={`/don/${p.orderId}`}>
                {`Mở đơn ${p.orderCode ?? ""}`.trim()}
              </Link>
            )}
            <button type="button" className="btn btn-text" onClick={onClose}>
              Chọn comment khác
            </button>
          </div>
        </div>
      );
    case "out_of_stock": {
      const l = lines.find((x) => x.hit.variantId === p.variantId);
      const n = p.available === null ? "" : ` (còn ${String(p.available)})`;
      return (
        <div className="stack-sm">
          <div className="box box-err small">
            <b>{`Hết hàng${n}${l === undefined ? "" : `: ${lineName(l.hit)}`}.`}</b> Chưa tạo đơn nháp, không giữ món
            nào.
          </div>
          <div className="row">
            <button type="button" className="btn btn-quiet" onClick={onFix}>
              Sửa số lượng
            </button>
          </div>
        </div>
      );
    }
    case "open_draft":
      return (
        <div className="stack-sm">
          {p.drafts.map((d) => (
            <div key={d.orderId} className="stack-sm">
              <div className="box box-soft small">
                <b>{`Khách đã có đơn nháp ${d.code} trong phiên này.`}</b>
                <br />
                {itemsText(d.items)} · <span className="money">{formatMoney(d.grandTotal)}</span>
              </div>
              <div className="row">
                <div className="act">
                  <button
                    type="button"
                    className="btn btn-dark"
                    disabled={!canUpdate || pending}
                    onClick={() => {
                      onAdd(d);
                    }}
                  >
                    {`Thêm vào đơn ${d.code}`}
                  </button>
                  {!canUpdate && <span className="reason">Cần quyền sửa đơn.</span>}
                </div>
                <button type="button" className="btn btn-quiet" disabled={pending} onClick={onNew}>
                  Tạo đơn mới
                </button>
              </div>
            </div>
          ))}
        </div>
      );
    case "seller_mismatch":
      return (
        <div className="box box-err small">
          <b>Sản phẩm không thuộc người bán của Page này.</b> Chọn sản phẩm khác.
        </div>
      );
    case "version_conflict":
      return (
        <div className="stack-sm">
          <div className="box box-err small">
            <b>Có người vừa sửa đơn nháp đó, tải lại.</b> Chưa thêm món nào.
          </div>
          <div className="row">
            <button type="button" className="btn btn-quiet" onClick={onFix}>
              Tải lại
            </button>
          </div>
        </div>
      );
    case "forbidden":
      return (
        <div className="box box-err small">
          Chưa có quyền lên đơn ở kho giữ hàng của đơn. Nhờ chủ shop gán quyền lên đơn ở kho này hoặc toàn shop.
        </div>
      );
  }
}

export function ClaimPanel({
  comment,
  pageId,
  canCreate,
  canUpdate,
  canReadStock,
  onClaimed,
  onClose,
}: ClaimPanelProps) {
  const qc = useQueryClient();
  const inputRef = useRef<HTMLInputElement>(null);
  const [lines, setLines] = useState<Line[]>([]);
  const [askAddress, setAskAddress] = useState(true);
  const [result, setResult] = useState<{ r: ClaimLiveCommentResult; added: boolean; lines: Line[] } | null>(null);
  const [problem, setProblem] = useState<ClaimProblem | null>(null);
  const [copied, setCopied] = useState<{ phone: string; ok: boolean } | null>(null);
  const others = usePresence(comment.conversationId);
  const tpl = useAskTemplate(pageId);
  const ctx = useQuery({
    queryKey: queryKey("live.comment-context", comment.messageId),
    queryFn: () => unwrap(getLiveCommentContext({ path: { id: comment.messageId } })),
  });
  const write = useWriteAction((key, b: ClaimBody) =>
    unwrap(claimLiveComment({ path: { id: b.commentId }, body: b.req, headers: { "Idempotency-Key": key } })),
  );

  const send = async (extra: Partial<ClaimLiveCommentRequest>) => {
    const req: ClaimLiveCommentRequest = {
      items: lines.map((l) => ({ variantId: l.hit.variantId, qty: l.qty })),
      mode: "new",
      askAddress,
      ...extra,
    };
    setProblem(null);
    const r = await write.run({ commentId: comment.messageId, req });
    if (!r.ok) {
      setProblem(claimProblem(r.error));
      return;
    }
    setResult({ r: r.data, added: req.mode === "add_to_draft", lines });
    onClaimed(comment.messageId, r.data);
    void qc.invalidateQueries({ queryKey: [PENDING_KEY] });
    void qc.invalidateQueries({ queryKey: ["live.comment-context"] });
  };

  const fix = () => {
    setProblem(null);
    write.reset();
    void ctx.refetch();
  };
  const addLine = (h: VariantSearchHit) => {
    setProblem(null);
    setLines((ls) =>
      ls.some((l) => l.hit.variantId === h.variantId)
        ? ls.map((l) => (l.hit.variantId === h.variantId ? { ...l, qty: Math.min(MAX_QTY, l.qty + 1) } : l))
        : [...ls, { hit: h, qty: 1 }],
    );
  };
  const setQty = (id: string, qty: number) => {
    setProblem(null);
    setLines((ls) =>
      qty <= 0 ? ls.filter((l) => l.hit.variantId !== id) : ls.map((l) => (l.hit.variantId === id ? { ...l, qty } : l)),
    );
  };
  const copy = async (phone: string) => {
    const ok = await navigator.clipboard.writeText(phone).then(
      () => true,
      () => false,
    );
    setCopied({ phone, ok });
  };

  const total = lines.reduce((s, l) => s + l.hit.price * l.qty, 0);
  const c = ctx.data?.customer;
  const presence = others.map((p) => `${p.displayName} ${PRESENCE_TEXT[p.state]}`).join(", ");
  const claimed = comment.claim;

  const head = (
    <div className="sec">
      <div className="row-between">
        <h3>{`Chốt comment ${clockTime(comment.sentAt)}`}</h3>
        <button type="button" className="btn btn-text" onClick={onClose}>
          Đóng
        </button>
      </div>
      {presence !== "" && <span className="xs muted">{presence}</span>}
      <div className="box box-info small lc-x">{`"${comment.text}"`}</div>
    </div>
  );

  const customer = (
    <div className="sec">
      {ctx.isError && <div className="box box-err small">Chưa tải được thông tin khách. Thử lại sau ít giây.</div>}
      <div className="row" style={{ flexWrap: "nowrap" }}>
        <span className="avatar" aria-hidden="true">
          {comment.customer.name.trim().charAt(0).toUpperCase()}
        </span>
        <div className="grow" style={{ minWidth: 0 }}>
          <b className="ellip" style={{ display: "block" }}>
            {c?.name ?? comment.customer.name}
          </b>
          {c !== undefined && (
            <span className="small muted">
              {c.orderCount === 0
                ? "Khách mới, chưa có đơn"
                : `${String(c.orderCount)} đơn · nhận ${String(c.deliveredCount)}/${String(c.orderCount)}`}
            </span>
          )}
        </div>
        <Link className="btn btn-quiet" to={`/inbox/${comment.conversationId}`}>
          Hội thoại
        </Link>
      </div>
      {(ctx.data?.phones ?? []).map((p) => (
        <div key={p} className="row">
          <span className="small muted">SĐT trong comment:</span>
          <button type="button" className="btn btn-quiet" onClick={() => void copy(p)}>
            {`Dùng số ${displayPhone(p)}`}
          </button>
        </div>
      ))}
      {copied !== null && (
        <p className="hint">
          {copied.ok
            ? `Đã chép số ${displayPhone(copied.phone)}, dán vào đơn khi hoàn tất ở khung hội thoại.`
            : `Ghi lại số ${displayPhone(copied.phone)} để điền vào đơn khi hoàn tất ở khung hội thoại.`}
        </p>
      )}
    </div>
  );

  if (claimed !== undefined && result === null) {
    return (
      <aside className="pane-side" aria-label="Khung chốt">
        {head}
        <div className="sec">
          <div className="box box-ok small">{`Đã lên đơn ${claimed.orderCode} bởi ${claimed.by.name}.`}</div>
          <div className="row">
            <Link className="btn btn-line" to={`/don/${claimed.orderId}`}>
              {`Mở đơn ${claimed.orderCode}`}
            </Link>
          </div>
        </div>
        {customer}
      </aside>
    );
  }

  if (result !== null) {
    const ask = askAfterClaim(result.r.ask);
    const shown = result.lines.map((l) => `${lineName(l.hit)} × ${String(l.qty)}`).join(", ");
    return (
      <aside className="pane-side" aria-label="Khung chốt">
        {head}
        <div className="sec">
          <div className="box box-ok small">
            <b className="ok-text">{`${result.added ? "Đã thêm vào đơn" : "Đã tạo đơn nháp"} ${result.r.orderCode}`}</b>
            {` · ${shown}`}
          </div>
          {ask.ok ? (
            <p className="small">
              <b>Xin địa chỉ:</b> {ask.text}
            </p>
          ) : (
            <div className="box box-info small">
              <b>Chưa xin được địa chỉ.</b> {ask.text}
            </div>
          )}
          <div className="row">
            <Link className="btn btn-line" to={`/don/${result.r.orderId}`}>
              {`Mở đơn ${result.r.orderCode}`}
            </Link>
            <Link className="btn btn-quiet" to={`/inbox/${comment.conversationId}`}>
              Mở hội thoại
            </Link>
          </div>
        </div>
        {customer}
      </aside>
    );
  }

  const askLabel =
    tpl.title === null
      ? tpl.loading
        ? "Nhắn riêng xin địa chỉ"
        : "Nhắn riêng xin địa chỉ (Page chưa chọn mẫu xin địa chỉ)"
      : tpl.title === ""
        ? "Nhắn riêng xin địa chỉ (mẫu của Page)"
        : `Nhắn riêng xin địa chỉ (mẫu "${tpl.title}")`;

  return (
    <aside className="pane-side" aria-label="Khung chốt">
      {head}
      {customer}
      <div className="sec">
        <span className="small">Sản phẩm (mã, tên, barcode)</span>
        <VariantPicker
          inputRef={inputRef}
          canReadStock={canReadStock}
          onPick={(h) => {
            addLine(h);
          }}
        />
        <p className="hint">Chỉ hàng của người bán của Page. "Có thể bán" là tồn trừ hàng đang giữ.</p>
      </div>
      <div className="sec">
        {lines.length > 0 && (
          <ul className="olines">
            {lines.map((l) => (
              <li key={l.hit.variantId}>
                <span className="ellip">{`${l.hit.sku} · ${lineName(l.hit)}`}</span>
                <span className="stepper">
                  <button
                    type="button"
                    className="ibtn"
                    aria-label={`Bớt ${l.hit.sku}`}
                    onClick={() => {
                      setQty(l.hit.variantId, l.qty - 1);
                    }}
                  >
                    −
                  </button>
                  <input
                    className="input"
                    aria-label={`Số lượng ${l.hit.sku}`}
                    inputMode="numeric"
                    value={l.qty}
                    onChange={(e) => {
                      const n = Number(e.target.value.replace(/\D/g, ""));
                      if (Number.isFinite(n) && n >= 1) setQty(l.hit.variantId, Math.min(MAX_QTY, n));
                    }}
                  />
                  <button
                    type="button"
                    className="ibtn"
                    aria-label={`Thêm ${l.hit.sku}`}
                    disabled={l.qty >= MAX_QTY}
                    onClick={() => {
                      setQty(l.hit.variantId, l.qty + 1);
                    }}
                  >
                    +
                  </button>
                </span>
              </li>
            ))}
          </ul>
        )}
        {lines.length > 0 && (
          <div className="sum">
            <div className="sum-r is-total">
              <span>Tạm tính</span>
              <span className="money">{formatMoney(total)}</span>
            </div>
          </div>
        )}
        <label className="check">
          <input
            type="checkbox"
            checked={askAddress}
            onChange={(e) => {
              setAskAddress(e.target.checked);
            }}
          />{" "}
          {askLabel}
        </label>
        <div className="act">
          <button
            type="button"
            className="btn btn-dark btn-wide"
            disabled={!canCreate || lines.length === 0 || write.pending}
            onClick={() => void send({})}
          >
            {askAddress ? "Tạo đơn nháp và xin địa chỉ" : "Tạo đơn nháp"}
          </button>
          {!canCreate && <span className="reason">Chưa có quyền tạo đơn.</span>}
        </div>
        <Problem
          p={problem}
          message={problem === null ? write.message : null}
          lines={lines}
          canUpdate={canUpdate}
          pending={write.pending}
          onAdd={(d) => void send({ mode: "add_to_draft", orderId: d.orderId, expectedVersion: d.version })}
          onNew={() => void send({ forceNew: true })}
          onFix={fix}
          onClose={onClose}
        />
        <p className="hint">Giữ hàng theo hạn đơn nháp ở Cài đặt đơn. Nút khóa khi đang gửi.</p>
      </div>
    </aside>
  );
}
