// Quá hạn chưa có tiền /cod/overdue (kế hoạch M3 Task 137; mockup docs/mockups/m3/doi-soat-cod.html trạng thái F). Máy
// chủ (cash.ListOverdue) chọn vận đơn giao xong quá số ngày của cài đặt vận chuyển mà hãng còn giữ tiền COD.
import { Link } from "react-router";
import { listCodOverdue } from "../../api";
import { unwrap } from "../../app/errors";
import { overdueKey } from "./codData";
import { carrierLine, formatMoney, vnDay } from "./codRules";
import { Paged, usePaged } from "./Paged";
import "./cod.css";

export function OverduePage() {
  const paged = usePaged(overdueKey(), (query) => unwrap(listCodOverdue({ query })));
  return (
    <div className="cod">
      <div className="page-h">
        <div>
          <p className="crumb">Tiền / Quá hạn chưa có tiền</p>
          <h1>Quá hạn chưa có tiền</h1>
          <p className="sub">Đơn đã giao quá hạn mà hãng còn giữ tiền thu hộ.</p>
        </div>
      </div>
      <Paged
        paged={paged}
        emptyTitle="Không có đơn quá hạn chưa có tiền"
        emptyText="Đơn giao quá 14 ngày mà hãng chưa chuyển tiền sẽ vào đây."
        countText={(n) => `${String(n)} đơn`}
      >
        <div className="tbl-frame">
          <table className="tbl cod-t">
            <thead>
              <tr>
                <th>Đơn · mã vận đơn</th>
                <th>Giao</th>
                <th className="r">Hãng đang giữ</th>
              </tr>
            </thead>
            <tbody>
              {paged.rows.map((o) => (
                <tr key={o.shipmentId}>
                  <td className="lead">
                    <span className="who-c">
                      {o.orderId === undefined ? (
                        <span className="num bold">{o.orderCode}</span>
                      ) : (
                        <Link className="cell-a num bold" to={`/don/${o.orderId}`}>
                          {o.orderCode}
                        </Link>
                      )}
                      <span className="xs muted num">{carrierLine(o.carrierKey, o.trackingCode)}</span>
                    </span>
                  </td>
                  <td data-label="Giao">
                    <span className="small num">
                      {`${vnDay(o.deliveredAt)} · `}
                      <b className="err-text">{`${String(o.days)} ngày`}</b>
                    </span>
                  </td>
                  <td data-label="Hãng đang giữ" className="r">
                    <span className="money">{formatMoney(o.carrierHolding)}</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Paged>
    </div>
  );
}
