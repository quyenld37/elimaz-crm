// Màn Đơn trùng /don/trung (kế hoạch M2 Task M2-28; mockup docs/mockups/m2/don-trung.html trạng thái A–C; HV25).
// Cặp đơn nghi trùng chưa xử lý, mới nhất trước; hai đơn cạnh nhau, sản phẩm chung tô nền. "Hủy đơn này" cần xác
// nhận (máy chủ hủy với lý do duplicate, gửi version của đơn đã đọc); "Không trùng" không cần xác nhận, có thông báo.
// Hệ thống không tự hủy. Đơn đã gửi hãng (có vận đơn hiện hành): máy chủ chỉ chuyển "Đang yêu cầu hủy", hàng chưa về
// kho, cặp còn ở danh sách tới khi hãng xác nhận (Task 432).
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import {
  listOrderDuplicates,
  resolveOrderDuplicate,
  type Order,
  type OrderDuplicate,
  type OrderListItem,
  type ResolveOrderDuplicateRequest,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { shortTime } from "../../app/time";
import { displayPhone } from "../len-don/orderRules";
import { formatMoney } from "../products/productRules";
import { Dialog } from "../staff/staffData";
import { LABEL_TEXT, labelChip } from "./labels";
import { ORDER_TOPIC, reloadOrder, useOrder, useOrderSettings, usePerms, useSources } from "./orderData";
import "./don.css";

const percent = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 1 });

interface ResolveBody {
  orderId: string;
  otherOrderId: string;
  req: ResolveOrderDuplicateRequest;
}

/** "Cách nhau 5 giờ 19 phút" từ hai mốc giờ. */
export function gapText(a: string, b: string): string {
  const min = Math.round(Math.abs(Date.parse(a) - Date.parse(b)) / 60_000);
  const h = Math.floor(min / 60);
  const m = min % 60;
  if (h === 0) return `Cách nhau ${String(m)} phút`;
  return m === 0 ? `Cách nhau ${String(h)} giờ` : `Cách nhau ${String(h)} giờ ${String(m)} phút`;
}

// Trạng thái hủy được (cancel.go): đơn đã gửi hãng thì máy chủ chuyển "Đang yêu cầu hủy".
const CANCELLABLE: ReadonlySet<OrderListItem["status"]> = new Set(["draft", "new", "confirmed"]);

const lineTotal = (it: Order["items"][number]) => it.netAmount + (it.allocatedOrderDiscount ?? 0);

function Side({
  row,
  detail,
  common,
  canResolve,
  onCancel,
}: {
  row: OrderListItem;
  detail: Order | undefined;
  common: ReadonlySet<string>;
  canResolve: boolean;
  onCancel: () => void;
}) {
  const sources = useSources();
  const a = detail?.shipAddress;
  const where = a === undefined ? "" : [a.line, a.wardName, a.provinceName].filter(Boolean).join(", ");
  const src = sources.nameOf(row.trafficSourceId);
  return (
    <div className="side-c">
      <div className="row-between">
        <Link className="cell-a bold num" to={`/don/${row.id}`}>
          {row.code}
        </Link>
        <span className={labelChip(row.label)}>{LABEL_TEXT[row.label]}</span>
      </div>
      <dl className="kv">
        <dt>Tạo lúc</dt>
        <dd className="num">{[shortTime(row.createdAt).replace(" ", " · "), src].filter(Boolean).join(" · ")}</dd>
        <dt>Khách</dt>
        <dd>{[row.customerName, detail?.customer?.code].filter(Boolean).join(" · ")}</dd>
        {a !== undefined && (
          <>
            <dt>Người nhận</dt>
            <dd>
              {a.name} · <span className="num">{displayPhone(a.phone)}</span>
            </dd>
            {where !== "" && (
              <>
                <dt>Giao tới</dt>
                <dd>{where}</dd>
              </>
            )}
          </>
        )}
      </dl>
      {detail === undefined ? (
        <p className="small muted">Đang tải dòng đơn…</p>
      ) : (
        <ul className="mini">
          {detail.items.map((it) => (
            <li key={it.id}>
              <span className={common.has(it.variantId) ? "same" : undefined}>{`${it.sku} × ${String(it.qty)}`}</span>
              <span className="money">{formatMoney(lineTotal(it))}</span>
            </li>
          ))}
        </ul>
      )}
      <div className="row-between">
        <b>Tổng</b>
        <span className="money">{formatMoney(row.grandTotal)}</span>
      </div>
      {canResolve && CANCELLABLE.has(row.status) && (
        <button type="button" className="btn btn-danger" disabled={detail === undefined} onClick={onCancel}>
          Hủy đơn này
        </button>
      )}
    </div>
  );
}

function Pair({
  p,
  canResolve,
  pending,
  onResolve,
}: {
  p: OrderDuplicate;
  canResolve: boolean;
  pending: boolean;
  onResolve: (req: ResolveOrderDuplicateRequest) => void;
}) {
  const left = useOrder(p.order.id).data;
  const right = useOrder(p.otherOrder.id).data;
  const [confirm, setConfirm] = useState<"cancel_this" | "cancel_other" | null>(null);
  const lv = new Set(left?.items.map((i) => i.variantId) ?? []);
  const common = new Set((right?.items ?? []).map((i) => i.variantId).filter((v) => lv.has(v)));
  const both = left !== undefined && right !== undefined;
  const sameCustomer = p.order.customerId === p.otherOrder.customerId;
  const target = confirm === "cancel_this" ? p.order : p.otherOrder;
  const kept = confirm === "cancel_this" ? p.otherOrder : p.order;
  const targetDetail = confirm === "cancel_this" ? left : right;
  const shipped = targetDetail?.currentShipmentId !== undefined;
  return (
    <article className="pair-c" aria-label={`${p.order.code} và ${p.otherOrder.code}`}>
      <div className="row-between">
        <div className="row">
          <b>{p.matchedBy === "customer" ? "Cùng khách" : "Cùng SĐT người nhận"}</b>
          {both && <span className="chip">{`Chung ${String(common.size)} sản phẩm`}</span>}
        </div>
        <span className="small muted">{gapText(p.order.createdAt, p.otherOrder.createdAt)}</span>
      </div>
      <div className="split">
        <Side
          row={p.order}
          detail={left}
          common={common}
          canResolve={canResolve}
          onCancel={() => {
            setConfirm("cancel_this");
          }}
        />
        <Side
          row={p.otherOrder}
          detail={right}
          common={common}
          canResolve={canResolve}
          onCancel={() => {
            setConfirm("cancel_other");
          }}
        />
      </div>
      <div className="row-between">
        <span className="small muted">
          {sameCustomer || p.matchedBy === "customer"
            ? 'Sản phẩm chung tô nền hồng. Hủy một đơn: lý do "Đơn trùng", nhả hàng và trả lượt mã của đơn đó (đơn đã gửi hãng: khi hãng xác nhận hủy).'
            : "Hai hồ sơ khách khác nhau dùng cùng một số: xem gợi ý gộp ở hồ sơ khách."}
        </span>
        {canResolve && (
          <button
            type="button"
            className="btn btn-line"
            disabled={pending}
            onClick={() => {
              onResolve({ action: "not_duplicate" });
            }}
          >
            Không trùng
          </button>
        )}
      </div>
      {confirm !== null && (
        <Dialog
          title={`Hủy đơn ${target.code}?`}
          onClose={() => {
            setConfirm(null);
          }}
          footer={
            <>
              <button
                type="button"
                className="btn btn-line"
                onClick={() => {
                  setConfirm(null);
                }}
              >
                Quay lại
              </button>
              <button
                type="button"
                className="btn btn-danger"
                disabled={pending || targetDetail === undefined}
                onClick={() => {
                  if (targetDetail === undefined) return;
                  onResolve({ action: confirm, version: targetDetail.version ?? 0 });
                  setConfirm(null);
                }}
              >
                {shipped ? `Gửi yêu cầu hủy ${target.code}` : `Hủy ${target.code}`}
              </button>
            </>
          }
        >
          {shipped ? (
            <p>
              {`Đơn đã gửi hãng. Đơn sẽ thành "Đang yêu cầu hủy" với lý do "Đơn trùng"; hàng chưa trả về kho, mã giảm giá chưa được trả lượt cho tới khi hãng xác nhận hủy. Hãng từ chối thì đơn quay lại "Đã xác nhận" và cặp vẫn ở đây. Đơn ${kept.code} giữ nguyên.`}
            </p>
          ) : (
            <p>
              {`Đơn bị hủy với lý do "Đơn trùng". Hàng của đơn trả về có thể bán ngay; mã giảm giá (nếu có) được trả lượt. Đơn ${kept.code} giữ nguyên.`}
            </p>
          )}
        </Dialog>
      )}
    </article>
  );
}

export function DuplicatePairs() {
  const qc = useQueryClient();
  const perms = usePerms();
  const settings = useOrderSettings();
  const [toast, setToast] = useState<string | null>(null);
  const key = queryKey(ORDER_TOPIC, "duplicates", "list");
  const list = useQuery({ queryKey: key, queryFn: () => unwrap(listOrderDuplicates({ query: { limit: 100 } })) });
  const write = useWriteAction((k, b: ResolveBody) =>
    unwrap(
      resolveOrderDuplicate({
        path: { orderId: b.orderId, otherOrderId: b.otherOrderId },
        body: b.req,
        headers: { "Idempotency-Key": k },
      }),
    ),
  );
  const canResolve = perms.can("order.duplicates.resolve");

  const resolve = async (p: OrderDuplicate, req: ResolveOrderDuplicateRequest) => {
    setToast(null);
    const r = await write.run({ orderId: p.order.id, otherOrderId: p.otherOrder.id, req });
    // Thành công hay bị từ chối (409 người khác vừa sửa đơn): đọc lại hai đơn và danh sách để thấy bản mới.
    reloadOrder(qc, p.order.id);
    reloadOrder(qc, p.otherOrder.id);
    void qc.invalidateQueries({ queryKey: queryKey(ORDER_TOPIC, "duplicates") });
    if (!r.ok) return;
    const after = req.action === "cancel_this" ? r.data.order : r.data.otherOrder;
    setToast(
      req.action === "not_duplicate"
        ? `Đã ghi ${p.order.code} và ${p.otherOrder.code} là không trùng.`
        : after.status === "cancel_requested"
          ? `Đã gửi yêu cầu hủy ${after.code}. Hàng chưa về kho cho tới khi hãng xác nhận; cặp còn ở danh sách tới lúc đó.`
          : `Đã hủy ${after.code}.`,
    );
  };

  const hours = settings.data === undefined ? null : percent.format(settings.data.duplicateWindowMinutes / 60);
  const items = list.data?.items ?? [];
  return (
    <div>
      <div className="page-h">
        <div>
          <p className="crumb">
            <Link to="/don">Đơn hàng</Link> / Đơn trùng
          </p>
          <h1>Đơn trùng</h1>
          <p className="sub">
            {`Cặp đơn cùng khách hoặc cùng SĐT người nhận, có ít nhất một sản phẩm chung, tạo cách nhau dưới ${
              hours === null ? "khoảng cài đặt" : `${hours} giờ`
            }. Hệ thống không tự hủy.`}
          </p>
        </div>
      </div>
      {toast !== null && (
        <div className="toast" role="status" style={{ marginBottom: 16 }}>
          <span className="grow">{toast}</span>
        </div>
      )}
      {write.message !== null && (
        <div className="box box-err" style={{ marginBottom: 16 }}>
          {write.message}
        </div>
      )}
      {list.isError && <div className="box box-err">{errorMessage(list.error)}</div>}
      {list.isSuccess && items.length === 0 && (
        <div className="empty">
          <h2>Không có đơn trùng cần xem</h2>
          <p className="muted">Cặp đã xử lý không hiện lại. Đổi khoảng thời gian ở Cài đặt đơn.</p>
        </div>
      )}
      {items.map((p) => (
        <Pair
          key={`${p.order.id}-${p.otherOrder.id}`}
          p={p}
          canResolve={canResolve}
          pending={write.pending}
          onResolve={(req) => {
            void resolve(p, req);
          }}
        />
      ))}
      {list.data?.nextCursor !== undefined && (
        <p className="hint" style={{ marginTop: 16 }}>
          Đang hiện 100 cặp mới nhất; xử lý bớt rồi tải lại trang để xem cặp cũ hơn.
        </p>
      )}
    </div>
  );
}
