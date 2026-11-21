// Lệch đối soát /cod/discrepancies (kế hoạch M3 Task 137; mockup docs/mockups/m3/doi-soat-cod.html trạng thái E; HV32;
// TI05). Máy chủ (cash.ListDiscrepancies) gom dòng lệch tiền còn mở, trùng, ngoài hệ thống của bảng kê đã chốt, phần
// thiếu còn mở, hãng chuyển tiền rồi báo hoàn. Không có nút "bỏ qua": chỉ đóng khi có bảng kê bù hoặc điều chỉnh COD
// (khối Tiền của đơn, quyền cash.adjust).
import { Link } from "react-router";
import { listCodDiscrepancies, type CodDiscrepancy } from "../../api";
import { unwrap } from "../../app/errors";
import { discrepanciesKey, usePerms } from "./codData";
import { carrierLine, discrepancyKind, formatMoney, signedMoney } from "./codRules";
import { Paged, usePaged } from "./Paged";
import "./cod.css";

function Row({ d, canAdjust }: { d: CodDiscrepancy; canAdjust: boolean }) {
  const kind = discrepancyKind(d);
  const fromLine = d.kind === "statement_line" && d.statementId !== undefined && d.lineNo !== undefined;
  const adjustable = canAdjust && d.orderId !== undefined && d.matchStatus !== "external";
  return (
    <tr>
      <td className="lead">
        <span className="who-c">
          {d.orderId === undefined ? (
            <span className="num bold">{d.orderCode ?? d.trackingCode ?? "—"}</span>
          ) : (
            <Link className="cell-a num bold" to={`/don/${d.orderId}`}>
              {d.orderCode ?? "Đơn"}
            </Link>
          )}
          <span className="xs muted num">{carrierLine(d.carrierKey, d.trackingCode)}</span>
        </span>
      </td>
      <td data-label="Loại">
        <span className="small">
          {kind}
          {fromLine && (
            <>
              {" · "}
              <Link to={`/cod/statements/${d.statementId ?? ""}`}>{`bảng kê dòng ${String(d.lineNo)}`}</Link>
            </>
          )}
        </span>
      </td>
      <td data-label="CRM chờ" className="r">
        {d.expectedAmount === undefined ? (
          <span className="muted">—</span>
        ) : (
          <span className="money">{formatMoney(d.expectedAmount)}</span>
        )}
      </td>
      <td data-label="Lệch" className="r">
        {d.diff === undefined ? (
          <span className="muted">—</span>
        ) : (
          <span className={d.diff < 0 ? "money err-text" : "money"}>{signedMoney(d.diff)}</span>
        )}
      </td>
      <td className="c-act">
        {adjustable && (
          <Link className="btn btn-line" to={`/don/${d.orderId ?? ""}`}>
            Điều chỉnh
          </Link>
        )}
      </td>
    </tr>
  );
}

export function DiscrepanciesPage() {
  const perms = usePerms();
  const paged = usePaged(discrepanciesKey(), (query) => unwrap(listCodDiscrepancies({ query })));
  return (
    <div className="cod">
      <div className="page-h">
        <div>
          <p className="crumb">Tiền / Lệch đối soát</p>
          <h1>Lệch đối soát</h1>
          <p className="sub">Dòng lệch tiền, phần thiếu còn mở, hãng chuyển tiền rồi báo hoàn.</p>
        </div>
      </div>
      <Paged
        paged={paged}
        emptyTitle="Không có lệch đối soát"
        emptyText="Dòng bảng kê lệch tiền, trùng, ngoài hệ thống sau khi chốt sẽ vào đây."
        countText={(n) => `${String(n)} mục mới nhất`}
      >
        <div className="tbl-frame">
          <table className="tbl cod-t">
            <thead>
              <tr>
                <th>Đơn · mã vận đơn</th>
                <th>Loại</th>
                <th className="r">CRM chờ</th>
                <th className="r">Lệch</th>
                <th className="c-act" />
              </tr>
            </thead>
            <tbody>
              {paged.rows.map((d, i) => (
                <Row
                  key={`${d.kind}-${d.statementId ?? ""}-${String(d.lineNo ?? i)}-${d.shipmentId ?? ""}`}
                  d={d}
                  canAdjust={perms.can("cash.adjust")}
                />
              ))}
            </tbody>
          </table>
        </div>
      </Paged>
      <p className="hint">Không có nút "bỏ qua": lệch chỉ đóng khi có bảng kê bù hoặc điều chỉnh COD có lý do.</p>
    </div>
  );
}
