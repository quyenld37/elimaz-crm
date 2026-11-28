// Số liệu trực tiếp của một đợt đã lên lịch, đang mở hay đã đóng (kế hoạch M5 Task 206; mockup
// docs/mockups/m5/flash-sales.html trạng thái E, F, G): sáu ô tổng, bảng từng món (suất, còn trống, đang giữ, đã bán, đã
// trả, người mua) với thanh chia phần. Màn không hiện tên, SĐT khách. Đang tải hay lỗi không vẽ như số 0.
import type { FlashItem, FlashLiveItem, FlashSaleStatus } from "../../api";
import { errorMessage } from "../../app/errors";
import { formatMoney } from "../products/productRules";
import { barPercents, liveTotals } from "./flashRules";
import { RetryBox } from "./RetryBox";

const num = (n: number) => new Intl.NumberFormat("vi-VN").format(n);

function Kpi(o: { label: string; value: number; className?: string }) {
  return (
    <div className={o.className === undefined ? "kpi" : `kpi ${o.className}`}>
      <p className="small muted">{o.label}</p>
      <p className="v">{num(o.value)}</p>
    </div>
  );
}

function ItemRow(o: {
  item: FlashItem;
  live: FlashLiveItem | undefined;
  status: FlashSaleStatus;
  canAddSlots: boolean;
  onAddSlots: (i: FlashItem) => void;
}) {
  const { item: i, live: l } = o;
  const bar = l === undefined ? null : barPercents(l);
  const cell = (label: string, v: number | undefined, extra = "") => (
    <td data-label={label} className={`r num${extra}`}>
      {v === undefined ? "—" : num(v)}
    </td>
  );
  return (
    <tr className={o.status === "open" && l?.available === 0 ? "is-err" : undefined}>
      <td className="lead">
        <div className="hold-l">
          <div className="sp">
            <span className="nm clamp2" title={i.name}>
              {i.name}
            </span>
            <span className="code">
              <span>{i.sku}</span>
              {" · "}
              <span className="money">{formatMoney(i.salePrice)}</span>
            </span>
            <span className="small muted">{`Mỗi khách ${String(i.perCustomerLimit)} · giữ ${String(i.holdMinutes)} phút`}</span>
          </div>
          {l !== undefined && bar !== null && (
            <span
              className="fs-bar"
              role="img"
              aria-label={`Đã bán ${String(l.sold)}, đang giữ ${String(l.held)}, còn trống ${String(l.available)} trên ${String(l.quantity)}`}
            >
              <i className="is-sold" style={{ width: `${String(bar.sold)}%` }} />
              <i className="is-held" style={{ width: `${String(bar.held)}%` }} />
            </span>
          )}
        </div>
      </td>
      {cell("Suất", l?.quantity ?? i.quantity)}
      {cell("Còn trống", l?.available, o.status === "open" && l?.available === 0 ? " err-text" : "")}
      {cell("Đang giữ", l?.held)}
      {cell("Đã bán", l?.sold)}
      {cell("Đã trả", l?.released)}
      {cell("Người mua", l?.buyers)}
      <td className="c-act">
        <div className="row">
          {o.canAddSlots && (
            <button
              type="button"
              className="btn btn-quiet"
              onClick={() => {
                o.onAddSlots(i);
              }}
            >
              Thêm suất
            </button>
          )}
        </div>
      </td>
    </tr>
  );
}

export function FlashSaleLive(o: {
  status: FlashSaleStatus;
  items: readonly FlashItem[];
  live: readonly FlashLiveItem[] | undefined;
  error: unknown;
  canAddSlots: boolean;
  onRetry: () => void;
  onAddSlots: (i: FlashItem) => void;
}) {
  if (o.live === undefined) {
    return o.error === null || o.error === undefined ? (
      <p className="muted" role="status">
        Đang tải số liệu…
      </p>
    ) : (
      <RetryBox title="Chưa tải được số liệu trực tiếp." text={errorMessage(o.error)} onRetry={o.onRetry} />
    );
  }
  const t = liveTotals({ items: [...o.live] });
  const byId = new Map(o.live.map((l) => [l.itemId, l]));
  return (
    <>
      <div className="kpis">
        <Kpi label="Suất" value={t.quantity} />
        {o.status !== "closed" && <Kpi label="Còn trống" value={t.available} />}
        {o.status !== "closed" && <Kpi label="Suất đang giữ" value={t.held} />}
        <Kpi label={o.status === "closed" ? "Đã bán" : "Suất đã bán"} value={t.sold} />
        <Kpi label="Đã trả về bán thường" value={t.released} />
        <Kpi label="Người mua (cộng theo món)" value={t.buyers} />
      </div>
      {o.status !== "closed" && (
        <div className="tbl-frame fs-gap">
          <table className="tbl fsi">
            <LiveHead />
            <tbody>
              {o.items.map((i) => (
                <ItemRow
                  key={i.id}
                  item={i}
                  live={byId.get(i.id)}
                  status={o.status}
                  canAddSlots={o.canAddSlots}
                  onAddSlots={o.onAddSlots}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {o.status !== "closed" && (
        <p className="small muted fs-gap">
          <span className="fs-key is-sold" /> Đã bán · <span className="fs-key is-held" /> Đang giữ ·{" "}
          <span className="fs-key" /> Còn trống. "Người mua" đếm số khách đã lấy suất (đang giữ hoặc đã bán) của từng
          món; khách mua hai món tính ở cả hai. Màn không hiện tên, SĐT khách.
        </p>
      )}
    </>
  );
}

function LiveHead() {
  return (
    <thead>
      <tr>
        <th>Món</th>
        <th className="r">Suất</th>
        <th className="r">Còn trống</th>
        <th className="r">Đang giữ</th>
        <th className="r">Đã bán</th>
        <th className="r">Đã trả</th>
        <th className="r">Người mua</th>
        <th className="c-act">Thao tác</th>
      </tr>
    </thead>
  );
}
