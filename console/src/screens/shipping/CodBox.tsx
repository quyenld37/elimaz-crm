// Khung tiền thu hộ (COD) bốn ngăn của vận đơn, chỉ đọc (kế hoạch M3 Task 134; mockup docs/mockups/m3/don-van-chuyen.html
// trạng thái D, E; HV15): Đang đi thu / Hãng đang giữ / Đã về tài khoản shop / Phí hãng, số lấy từ getShipmentCod (sổ
// COD chỉ ghi thêm của Task 120). Cần quyền cash.cod.read. Task 138 (mockup docs/mockups/m3/tien-tren-don.html trạng thái
// C, D): dưới bốn ngăn là sổ COD của vận đơn (mới nhất trên đầu, bút toán đảo kèm lý do và người làm) và nút "Điều chỉnh
// COD" (cash.adjust) mở hộp hai bước.
import { useState } from "react";
import type { CodBalances, Shipment } from "../../api";
import { errorMessage } from "../../app/errors";
import { shortTime } from "../../app/time";
import { CodAdjustmentDialog } from "../payments/CodAdjustmentDialog";
import { KIND_TEXT, entriesText, newestFirst } from "../payments/codLedger";
import { formatMoney } from "../products/productRules";
import { useShipmentCod } from "./useShipment";

const CELLS: readonly { key: keyof CodBalances; text: string; flow: boolean }[] = [
  { key: "toCollect", text: "Đang đi thu", flow: true },
  { key: "carrierHolding", text: "Hãng đang giữ", flow: true },
  { key: "shopReceived", text: "Đã về tài khoản shop", flow: true },
  { key: "carrierFee", text: "Phí hãng", flow: false },
];

/** Khung COD của vận đơn s của đơn orderCode; canRead null: quyền đang tải; false → câu không có quyền, không đọc. */
export function CodBox({
  s,
  orderCode,
  canRead,
  canAdjust,
}: {
  s: Shipment;
  orderCode: string;
  canRead: boolean | null;
  canAdjust: boolean;
}) {
  const q = useShipmentCod(s.id, canRead === true);
  const [adjust, setAdjust] = useState(false);
  return (
    <section className="card" aria-label="Tiền thu hộ (COD)">
      <div className="row-between">
        <h2>Tiền thu hộ (COD)</h2>
        {canRead === true && canAdjust && (
          <button
            type="button"
            className="btn btn-line"
            onClick={() => {
              setAdjust(true);
            }}
          >
            Điều chỉnh COD
          </button>
        )}
      </div>
      {canRead === null ? (
        <p className="small muted">Đang tải số tiền thu hộ…</p>
      ) : !canRead ? (
        <p className="small muted">Số tiền thu hộ chỉ hiện với người được xem COD (quyền "Xem tiền COD").</p>
      ) : q.isError ? (
        <div className="box box-err small">{errorMessage(q.error)}</div>
      ) : q.data === undefined ? (
        <p className="small muted">Đang tải số tiền thu hộ…</p>
      ) : (
        <>
          <div className="cod" style={{ marginTop: 8 }}>
            {CELLS.map((c) => {
              const v = q.data.balances[c.key];
              const cls = c.flow && v < 0 ? "cod-c is-bad" : c.flow && v > 0 ? "cod-c is-on" : "cod-c";
              return (
                <div key={c.key} className={cls} aria-label={c.text}>
                  <span className="xs muted">{c.text}</span>
                  <span className="money">{formatMoney(v)}</span>
                </div>
              );
            })}
          </div>
          {q.data.transfers.length > 0 && (
            <>
              <div className="row-between" style={{ marginTop: 12 }}>
                <p className="label">{`Sổ COD · ${s.trackingCode ?? s.code}`}</p>
                <span className="chip chip-line">Chỉ ghi thêm</span>
              </div>
              <ul className="tl" aria-label="Sổ COD">
                {newestFirst(q.data.transfers).map((t) => {
                  const by = [t.reason === undefined ? "" : `Lý do: ${t.reason}`, t.actor?.name ?? ""]
                    .filter((x) => x !== "")
                    .join(" · ");
                  return (
                    <li key={t.id}>
                      <span className="at">{shortTime(t.occurredAt)}</span>
                      <div>
                        <b>{KIND_TEXT[t.kind]}</b> <span className="small">{entriesText(t)}</span>
                        {t.kind === "reversal" && by !== "" && (
                          <>
                            <br />
                            <span className="small wrap-any">{by}</span>
                          </>
                        )}
                      </div>
                    </li>
                  );
                })}
              </ul>
            </>
          )}
        </>
      )}
      <p className="hint">Số trong ngăn tính từ sổ COD (chỉ ghi thêm). Phí hãng ghi khi có bảng kê.</p>
      {adjust && (
        <CodAdjustmentDialog
          s={s}
          orderCode={orderCode}
          onClose={() => {
            setAdjust(false);
          }}
        />
      )}
    </section>
  );
}
