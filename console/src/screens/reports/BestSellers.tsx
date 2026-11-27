// Bán chạy /bao-cao/ban-chay (Task 240, M6-17; mockup docs/m6/mockup/ban-chay.html; BC02). Sản phẩm bán nhiều nhất trong
// kỳ theo mã hàng (SKU) hay theo mẫu; cơ sở "đơn lên chưa hủy" (theo ngày lên đơn) hay "đơn thành công" (theo ngày hoàn
// tất). 50 dòng đầu; "Xem thêm" lấy tới 500 dòng bằng một lần gọi riêng (limit=500; tệp Excel có đủ mọi dòng). Hàng
// khách trả ở cột riêng, không trừ ngược vào kỳ bán. Thứ tự và số do máy chủ trả.
import { useState } from "react";
import { getSalesBestSellers, type SalesBestSellerRow, type SalesBestSellersReport } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { ExportButton } from "./ExportButton";
import { ReportBanners } from "./ReportBanners";
import { ReportPeriodPicker, SalesReportPage } from "./SalesReportPage";
import { clockTime, count, periodText, vnd } from "./format";
import { isBestSellers } from "./salesTables";
import { useReport } from "./useReport";
import { useReportPeriod } from "./useReportPeriod";
import { useExportRun } from "./useRunNotice";

export const FIRST_LIMIT = 50;
export const MAX_LIMIT = 500;
export const TEXT_EMPTY = "Chưa bán sản phẩm nào trong kỳ";
const SUB =
  "Sản phẩm bán nhiều nhất trong kỳ, theo mã hàng (SKU) hoặc theo mẫu. Hàng khách trả ở cột riêng, không trừ ngược vào kỳ bán.";

type Level = SalesBestSellersReport["level"];
type Basis = SalesBestSellersReport["basis"];
const parseLevel = (v: string | null): Level => (v === "product" ? "product" : "sku");
const parseBasis = (v: string | null): Basis => (v === "delivered" ? "delivered" : "placed");

function Name({ text }: { text: string }) {
  return (
    <span className="nm is-wide" title={text}>
      {text}
    </span>
  );
}

function BestTable({ rows, level }: { rows: readonly SalesBestSellerRow[]; level: Level }) {
  const sku = level === "sku";
  return (
    <div className="rt-frame scroll-x">
      <table className="rt" aria-label="Bán chạy">
        <thead>
          <tr>
            <th>#</th>
            {sku && <th className="l">SKU</th>}
            <th className="l">{sku ? "Tên" : "Mẫu"}</th>
            {sku && <th className="l">Màu / size</th>}
            <th>Số lượng</th>
            <th>Số đơn</th>
            <th>Doanh số</th>
            <th>Hàng khách trả</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={`${r.productId}|${r.variantId ?? ""}`}>
              <td>{i + 1}</td>
              {sku && <td className="l">{r.sku ?? "—"}</td>}
              <td className="l">
                <Name text={r.name} />
              </td>
              {sku && (
                <td className="l">{[r.color, r.size].filter((x) => x !== undefined && x !== "").join(" / ") || "—"}</td>
              )}
              <td>{count(r.quantity)}</td>
              <td>{count(r.orders)}</td>
              <td>
                <span className="money">{vnd(r.goodsAmount)}</span>
              </td>
              <td>{count(r.returnedQuantity)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

interface Expanded {
  /** Khóa của lần tải đầu mà bản 500 dòng này thuộc về (tải lại, đổi tham số thì bản này bị bỏ). */
  forKey: string;
  data: SalesBestSellersReport;
}

export function BestSellers() {
  const rp = useReportPeriod();
  const { sp, setSp, period, bad } = rp;
  const level = parseLevel(sp.get("xem"));
  const basis = parseBasis(sp.get("tinh"));
  const base = { from: period.from, to: period.to, level, basis };

  const rep = useReport<SalesBestSellersReport>({
    report: "best_sellers",
    params: base,
    load: () => unwrap(getSalesBestSellers({ query: { ...base, limit: FIRST_LIMIT } })),
    parse: (v) => (isBestSellers(v) ? v : null),
    enabled: bad === null,
  });
  const exp = useExportRun({
    report: "best_sellers",
    params: base,
    title: `Bán chạy ${periodText(period.from, period.to)}`,
  });

  const [expanded, setExpanded] = useState<Expanded | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreError, setMoreError] = useState<string | null>(null);
  const forKey = `${JSON.stringify(base)}|${String(rep.updatedAt?.getTime() ?? 0)}`;
  const full = expanded !== null && expanded.forKey === forKey ? expanded.data : null;

  const loadMore = () => {
    if (loadingMore) return;
    setLoadingMore(true);
    setMoreError(null);
    void unwrap(getSalesBestSellers({ query: { ...base, limit: MAX_LIMIT } })).then(
      (r) => {
        setLoadingMore(false);
        if (r.status === "ready") setExpanded({ forKey, data: r });
        else setMoreError("Báo cáo nặng, chưa lấy được thêm dòng. Thu hẹp kỳ rồi thử lại.");
      },
      (e: unknown) => {
        setLoadingMore(false);
        setMoreError(errorMessage(e));
      },
    );
  };

  const update = (change: (next: URLSearchParams) => void) => {
    const next = new URLSearchParams(sp);
    change(next);
    setSp(next);
  };
  const loading = rep.phase === "loading" && bad === null;
  const shown = rep.phase === "ready" ? (full ?? rep.data) : null;
  const unit = level === "sku" ? "SKU" : "mẫu";

  return (
    <SalesReportPage title="Bán chạy" sub={SUB} rep={rep} exp={exp} periodBad={bad !== null}>
      <ReportPeriodPicker rp={rp} disabled={loading} />
      <div className="rep-f">
        <div className="field">
          <span>Xem theo</span>
          <div className="row" role="group" aria-label="Xem theo">
            {(["sku", "product"] as const).map((l) => (
              <button
                key={l}
                type="button"
                className="toggle"
                aria-pressed={level === l}
                onClick={() => {
                  update((n) => {
                    n.set("xem", l);
                  });
                }}
              >
                {l === "sku" ? "SKU" : "Mẫu"}
              </button>
            ))}
          </div>
        </div>
        <div className="field">
          <label htmlFor="rp-best-basis">Tính trên</label>
          <span className="select-wrap">
            <select
              id="rp-best-basis"
              value={basis}
              onChange={(e) => {
                update((n) => {
                  n.set("tinh", parseBasis(e.target.value));
                });
              }}
            >
              <option value="placed">Đơn lên chưa hủy</option>
              <option value="delivered">Đơn thành công</option>
            </select>
          </span>
        </div>
      </div>
      <ReportBanners rep={rep} failedHint="Thu hẹp kỳ, rồi bấm Tải lại." />
      {shown !== null && rep.updatedAt !== null && (
        <>
          <div className="meta">
            <span>
              Theo <b>{basis === "delivered" ? "ngày hoàn tất" : "ngày lên đơn"}</b> ·{" "}
              {periodText(period.from, period.to)}; hàng khách trả theo <b>ngày nhận trả</b>
            </span>
            <span>
              Cập nhật lúc <b>{clockTime(rep.updatedAt)}</b>
            </span>
          </div>
          {shown.rows.length === 0 ? (
            <div className="empty">
              <h2>{TEXT_EMPTY}</h2>
              <p className="muted">Chọn kỳ khác hoặc đổi sang &quot;Đơn lên chưa hủy&quot;.</p>
            </div>
          ) : (
            <>
              <BestTable rows={shown.rows} level={level} />
              <BestBar
                shown={shown.rows.length}
                total={shown.totalRows}
                unit={unit}
                busy={loadingMore}
                onMore={loadMore}
                exportButton={<ExportButton exp={exp} disabled={loading || bad !== null} />}
              />
              {moreError !== null && <p className="field-err">{moreError}</p>}
            </>
          )}
          <p className="small muted">Doanh số = tiền hàng sau giảm giá, chưa gồm ship; chưa phải doanh thu kế toán.</p>
          {level === "product" && (
            <p className="xs muted">
              Xếp theo số lượng giảm dần; bằng nhau thì theo doanh số. Mức mẫu không có cột SKU, màu, size.
            </p>
          )}
        </>
      )}
    </SalesReportPage>
  );
}

interface BarProps {
  shown: number;
  total: number;
  unit: string;
  busy: boolean;
  onMore: () => void;
  exportButton: React.ReactNode;
}

/** Hàng dưới bảng: còn dòng chưa hiện thì "Xem thêm" (tới 500); đã tới 500 mà còn nữa thì nói tệp Excel có đủ. */
function BestBar({ shown, total, unit, busy, onMore, exportButton }: BarProps) {
  if (shown >= total)
    return (
      <div className="more">
        <span>{`Đủ ${count(total)} ${unit} có bán trong kỳ`}</span>
      </div>
    );
  if (shown >= MAX_LIMIT) {
    return (
      <div className="more">
        <span>{`Đang hiện ${count(shown)} dòng (tối đa trên màn hình). Tệp Excel có đủ mọi dòng.`}</span>
        {exportButton}
      </div>
    );
  }
  return (
    <div className="more">
      <span>{`Đang hiện ${count(shown)} dòng đầu / ${count(total)} ${unit} có bán trong kỳ`}</span>
      <button type="button" className="btn btn-quiet" disabled={busy} onClick={onMore}>
        {`Xem thêm (tới ${String(MAX_LIMIT)} dòng)`}
      </button>
    </div>
  );
}
