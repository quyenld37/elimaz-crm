// Chi tiết Yêu cầu về đơn hàng /don/yeu-cau/:id (kế hoạch M5 Task 207, M5-24; mockup docs/mockups/m5/order-requests.html
// trạng thái B–F; HV04, HV05, HV07, HV08). Đơn và khách đọc thêm từ getOrder khi người xem có order.read. Biến thể muốn
// đổi sang kèm số còn ở kho của đơn, thẻ Lịch sử (Task 486). Ảnh mở bằng link ký có hạn 10 phút ở tab mới, không nhúng.
import { useState } from "react";
import { Link, useParams } from "react-router";
import type { Order, OrderRequest } from "../../api";
import { errorMessage } from "../../app/errors";
import { shortTime } from "../../app/time";
import { formatMoney } from "../products/productRules";
import { useRequest, useRequestOrder, usePerms } from "./requestData";
import {
  KIND_TITLE,
  STATUS_TEXT,
  isClosed,
  lineReasonText,
  reasonText,
  safeImageUrl,
  statusChip,
} from "./requestRules";
import { History } from "./History";
import { StatusPanel } from "./StatusPanel";
import { Thread } from "./Thread";
import "../don/don.css";
import "./order-requests.css";

function OrderCard({ r, o, canOrder }: { r: OrderRequest; o: Order | undefined; canOrder: boolean }) {
  return (
    <section className="card">
      <h2>Đơn và khách</h2>
      <dl className="kv">
        <dt>Đơn</dt>
        <dd>
          {canOrder ? (
            <Link className="cell-a num" to={`/don/${r.orderId}`}>
              {r.orderCode}
            </Link>
          ) : (
            <span className="num">{r.orderCode}</span>
          )}
          {o !== undefined && (
            <>
              {" · "}
              <span className="money">{formatMoney(o.grandTotal)}</span>
            </>
          )}
        </dd>
        {o?.customer !== undefined && (
          <>
            <dt>Khách</dt>
            <dd>
              <span className="wrap-any">{o.customer.fullName}</span>
            </dd>
          </>
        )}
        <dt>Lý do</dt>
        <dd>{reasonText(r.reasonCode)}</dd>
      </dl>
    </section>
  );
}

function Lines({ r }: { r: OrderRequest }) {
  if (r.lines.length === 0) return null;
  const exchange = r.kind === "exchange";
  return (
    <section className="card">
      <h2>{exchange ? "Món khách muốn đổi" : "Món trong yêu cầu"}</h2>
      <div className="tbl-frame">
        <table className="tbl">
          <thead>
            <tr>
              <th>Món đã mua</th>
              <th>SL</th>
              <th>Lý do</th>
              {exchange && <th>Muốn đổi sang</th>}
            </tr>
          </thead>
          <tbody>
            {r.lines.map((l) => (
              <tr key={l.variantId}>
                <td className="lead">
                  <div className="rq-sp">
                    <span className="wrap-any">{l.name ?? "Biến thể"}</span>
                    {l.sku !== undefined && <span className="small muted num">{l.sku}</span>}
                  </div>
                </td>
                <td data-label="SL" className="num">
                  {l.qty}
                </td>
                <td data-label="Lý do">
                  <div className="rq-sp">
                    <span>{lineReasonText(l.reason)}</span>
                    {l.note !== undefined && l.note !== "" && <span className="small muted wrap-any">{l.note}</span>}
                  </div>
                </td>
                {exchange && (
                  <td data-label="Muốn đổi sang">
                    {l.wantVariantId === undefined || l.wantVariantId === l.variantId ? (
                      <span className="muted">Cùng biến thể</span>
                    ) : l.want === undefined ? (
                      <span>Biến thể khác (không còn trong danh mục)</span>
                    ) : (
                      <div className="rq-sp">
                        <span className="wrap-any">{`${l.want.color} / ${l.want.size}`}</span>
                        <span className="small muted num">{l.want.sku}</span>
                        {l.want.available !== undefined && l.want.warehouseName !== undefined && (
                          <span className={l.want.available > 0 ? "small" : "small err-text"}>
                            {l.want.available > 0
                              ? `còn ${String(l.want.available)} ở ${l.want.warehouseName}`
                              : `hết ở ${l.want.warehouseName}`}
                          </span>
                        )}
                      </div>
                    )}
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function Photos({ r }: { r: OrderRequest }) {
  if (r.files.length === 0) return null;
  return (
    <section className="card">
      <h2>{`Ảnh khách gửi (${String(r.files.length)})`}</h2>
      <div className="rq-imgs">
        {r.files.map((f, i) => {
          const url = safeImageUrl(f);
          if (url !== null) {
            return (
              <div key={f.fileId} className="img-tile">
                <div className="img-ph">{`Ảnh ${String(i + 1)}`}</div>
                <div className="img-b">
                  <a className="btn btn-quiet" href={url} target="_blank" rel="noopener noreferrer">
                    Mở ảnh
                  </a>
                  <span className="xs muted">Mở ở tab mới · link có hạn 10 phút</span>
                </div>
              </div>
            );
          }
          const [cls, title, note] =
            f.status === "processing"
              ? ["img-ph is-proc", "Đang xử lý ảnh…", "Ảnh sẽ hiện khi xử lý xong"]
              : f.status === "rejected"
                ? ["img-ph is-bad", "Ảnh bị từ chối", "Tệp không phải ảnh hợp lệ, không mở được"]
                : f.status === "missing"
                  ? ["img-ph is-lost", "Ảnh không còn", "Tệp đã bị dọn hoặc hết hạn"]
                  : ["img-ph is-bad", `Ảnh ${String(i + 1)}`, "Không mở được"];
          return (
            <div key={f.fileId} className="img-tile">
              <div className={cls}>{title}</div>
              <div className="img-b">
                <span className={f.status === "rejected" ? "xs err-text" : "xs muted"}>{note}</span>
              </div>
            </div>
          );
        })}
      </div>
    </section>
  );
}

/** Hộp trạng thái đã đóng / hướng dẫn gửi trả / yêu cầu hủy đơn đã gửi hãng (mockup C, E). */
function StateBox({ r, o, canOrder }: { r: OrderRequest; o: Order | undefined; canOrder: boolean }) {
  const shippedCancel = r.kind === "cancel" && !isClosed(r.status) && o?.currentShipmentId !== undefined;
  return (
    <>
      {r.status === "cancelled" && (
        <div className="box box-info">
          <b>Khách đã rút yêu cầu trên website</b>
          {r.resolvedAt === undefined ? "" : ` lúc ${shortTime(r.resolvedAt)}`}. Không cần xử lý.
        </div>
      )}
      {(r.status === "resolved" || r.status === "rejected") && (
        <div className="box box-info wrap-any">
          <b>Yêu cầu đã đóng.</b>
          {r.resolvedAt === undefined ? "" : ` ${STATUS_TEXT[r.status]} lúc ${shortTime(r.resolvedAt)}.`}
          {r.outcome?.reason !== undefined && ` Lý do khách thấy: "${r.outcome.reason}"`}
          {r.outcome?.trackingCode !== undefined && ` Mã vận đơn đơn đổi: ${r.outcome.trackingCode}.`}
        </div>
      )}
      {r.shipBack !== undefined && (
        <div className="box box-info stack-sm">
          <b>Khách gửi hàng về</b>
          <dl className="kv">
            <dt>Địa chỉ</dt>
            <dd className="wrap-any">{r.shipBack.address}</dd>
            <dt>Mã trên kiện</dt>
            <dd className="num">{r.shipBack.packageCode}</dd>
            <dt>Gửi trước</dt>
            <dd className="num">{shortTime(r.shipBack.deadline)}</dd>
          </dl>
        </div>
      )}
      {shippedCancel && (
        <section className="card">
          <p>
            {`Đơn ${r.orderCode} `}
            <b>đã gửi hãng</b> nên website không tự hủy được; khách gửi yêu cầu hủy.
          </p>
          {canOrder && (
            <div className="acts">
              <Link className="btn btn-dark" to={`/don/${r.orderId}`}>
                Mở đơn để hủy vận đơn
              </Link>
            </div>
          )}
          <p className="hint">
            Hủy đơn, đổi hãng làm ở màn Đơn (khối Vận chuyển). Xong thì quay lại đây chọn Hoàn tất.
          </p>
        </section>
      )}
    </>
  );
}

function Body({ r, perms }: { r: OrderRequest; perms: ReturnType<typeof usePerms> }) {
  const canOrder = perms.can("order.read");
  const canManage = perms.can("order.requests.manage");
  const order = useRequestOrder(r.orderId, canOrder);
  const o = order.data;
  const [busy, setBusy] = useState(false);
  const closed = isClosed(r.status);
  return (
    <div>
      <p className="crumb">
        <Link to="/don/yeu-cau">Yêu cầu về đơn hàng</Link> / {r.ref}
      </p>
      <header className="page-h">
        <div className="grow">
          <div className="row">
            <h1>{`${KIND_TITLE[r.kind]} ${r.ref}`}</h1>
            <span className={statusChip(r.status)}>{STATUS_TEXT[r.status]}</span>
          </div>
          <p className="sub">{`Gửi từ website lúc ${shortTime(r.createdAt)} · cập nhật ${shortTime(r.updatedAt)}`}</p>
        </div>
        {canOrder && (
          <div className="row">
            <Link className="btn btn-line" to={`/don/${r.orderId}`}>
              {`Mở đơn ${r.orderCode}`}
            </Link>
          </div>
        )}
      </header>
      <div className="cols">
        <div className="stack">
          <StateBox r={r} o={o} canOrder={canOrder} />
          <OrderCard r={r} o={o} canOrder={canOrder} />
          <Lines r={r} />
          <Photos r={r} />
          <Thread r={r} canWrite={canManage && !closed} busy={busy} onPending={setBusy} />
        </div>
        <aside className="stack">
          {!closed && canManage && <StatusPanel r={r} busy={busy} onPending={setBusy} />}
          {!closed && !canManage && (
            <section className="card">
              <h2>Xử lý yêu cầu</h2>
              <p className="small muted">
                Bạn chỉ có quyền xem yêu cầu (thiếu quyền &quot;Xử lý yêu cầu về đơn hàng&quot;). Không gửi tin, không
                đổi trạng thái được; nhờ Quản lý cấp quyền.
              </p>
            </section>
          )}
          <History r={r} />
        </aside>
      </div>
    </div>
  );
}

export function RequestDetail() {
  const { id = "" } = useParams();
  const q = useRequest(id);
  const perms = usePerms();
  if (q.isError) {
    return (
      <div className="box box-err stack-sm">
        <b>Chưa tải được yêu cầu.</b>
        <p>{errorMessage(q.error)}</p>
        <div>
          <Link className="btn btn-line" to="/don/yeu-cau">
            Về danh sách yêu cầu
          </Link>
        </div>
      </div>
    );
  }
  // Chờ cả quyền: không vẽ tạm như "chỉ xem" rồi mới hiện thẻ xử lý.
  if (q.data === undefined || !perms.loaded) return <p className="muted">Đang tải yêu cầu…</p>;
  return <Body r={q.data} perms={perms} />;
}
