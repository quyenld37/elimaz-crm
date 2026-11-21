// Khung tiền thu hộ (COD) bốn ngăn của vận đơn, chỉ đọc (kế hoạch M3 Task 134; mockup docs/mockups/m3/don-van-chuyen.html
// trạng thái D, E; HV15): Đang đi thu / Hãng đang giữ / Đã về tài khoản shop / Phí hãng, số lấy từ getShipmentCod (sổ
// COD chỉ ghi thêm của Task 120). Đối soát COD tạm dừng: khung chỉ hiện số, không có thao tác. Cần quyền cash.cod.read.
import type { CodBalances } from "../../api";
import { errorMessage } from "../../app/errors";
import { formatMoney } from "../products/productRules";
import { useShipmentCod } from "./useShipment";

const CELLS: readonly { key: keyof CodBalances; text: string; flow: boolean }[] = [
  { key: "toCollect", text: "Đang đi thu", flow: true },
  { key: "carrierHolding", text: "Hãng đang giữ", flow: true },
  { key: "shopReceived", text: "Đã về tài khoản shop", flow: true },
  { key: "carrierFee", text: "Phí hãng", flow: false },
];

/** Khung COD của vận đơn id; canRead null: quyền đang tải; false → câu không có quyền, không đọc. */
export function CodBox({ id, canRead }: { id: string; canRead: boolean | null }) {
  const q = useShipmentCod(id, canRead === true);
  return (
    <section className="card" aria-label="Tiền thu hộ (COD)">
      <h2>Tiền thu hộ (COD)</h2>
      {canRead === null ? (
        <p className="small muted">Đang tải số tiền thu hộ…</p>
      ) : !canRead ? (
        <p className="small muted">Số tiền thu hộ chỉ hiện với người được xem COD (quyền "Xem tiền COD").</p>
      ) : q.isError ? (
        <div className="box box-err small">{errorMessage(q.error)}</div>
      ) : q.data === undefined ? (
        <p className="small muted">Đang tải số tiền thu hộ…</p>
      ) : (
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
      )}
      <p className="hint">Số trong ngăn tính từ sổ COD (chỉ ghi thêm). Phí hãng ghi khi có bảng kê.</p>
    </section>
  );
}
