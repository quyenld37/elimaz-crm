// Báo cáo đơn /bao-cao/don (Task 240, M6-17; mockup docs/m6/mockup/don.html trạng thái A–F; BC02). Chọn kỳ, cơ sở ngày
// (lên đơn / hoàn tất), chiều nhóm (tám chiều), bộ lọc nhiều lựa chọn; bảng có dòng Tổng; mỗi ô số bấm được mở danh sách
// chi tiết của đúng ô đó (hành vi (a)); cơ sở hoàn tất ẩn các cột máy chủ trả null. Mọi số do máy chủ tính; màn chỉ
// định dạng. Kỳ, cơ sở, nhóm, bộ lọc, ô đang mở nằm trên địa chỉ.
import { useQuery } from "@tanstack/react-query";
import {
  getSalesOrdersReport,
  getSalesReportFilters,
  type ReportDimension,
  type SalesOrdersReport,
  type SalesOrdersRow,
} from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { CellNumber, MoneyCell } from "./CellNumber";
import { DrilldownPanel } from "./DrilldownPanel";
import { MultiFilter } from "./MultiFilter";
import { ReportBanners } from "./ReportBanners";
import { ReportPeriodPicker, SalesReportPage } from "./SalesReportPage";
import { parseCell, writeCell, type Basis, type Cell } from "./cells";
import { clockTime, count, periodText, rate, vnd } from "./format";
import {
  GROUPS,
  filterQuery,
  groupLabel,
  hasFilters,
  isOrdersReport,
  noFilters,
  parseFilters,
  parseGroup,
  writeFilters,
  type OrderFilters,
} from "./salesTables";
import { useReport } from "./useReport";
import { useReportPeriod } from "./useReportPeriod";
import { useExportRun } from "./useRunNotice";

export const TEXT_EMPTY = "Không có đơn khớp bộ lọc";
const SUB =
  "Đơn theo trạng thái, nguồn, nhân viên, kênh, Page, hãng hoặc ngày. Tổng của bảng bằng tổng các ô, bằng số dòng của danh sách chi tiết khi bấm vào ô.";

const parseBasis = (v: string | null): Basis => (v === "completed" ? "completed" : "placed");

interface TableProps {
  d: SalesOrdersReport;
  basis: Basis;
  open: Cell | null;
  onOpen: (c: Cell) => void;
}

/** Một ô số của dòng r: bấm được khi có đơn (n > 0), kèm chiều và khóa của dòng. */
function Num({
  r,
  n,
  text,
  metric,
  money = false,
  p,
}: {
  r: SalesOrdersRow;
  n: number;
  text: string;
  metric: Cell["metric"];
  money?: boolean;
  p: TableProps;
}) {
  const cell: Cell = { metric, basis: p.basis, dimension: p.d.groupBy, key: r.key };
  return (
    <td>
      <CellNumber n={n} text={text} money={money} cell={cell} open={p.open} onOpen={p.onOpen} />
    </td>
  );
}

function OrdersTable(p: TableProps) {
  const { d } = p;
  // Cột máy chủ trả null (cơ sở hoàn tất) thì ẩn, không vẽ "0" thay cho "không có".
  const hasPlaced = d.totals.placed !== null;
  const hasActive = d.totals.active !== null;
  const hasCancelled = d.totals.cancelled !== null;
  return (
    <div className="rt-frame scroll-x">
      <table className="rt" aria-label="Báo cáo đơn">
        <thead>
          <tr>
            <th>{groupLabel(d.groupBy)}</th>
            {hasPlaced ? <th>Đơn lên</th> : <th>Số đơn</th>}
            <th>{hasActive ? "Doanh số" : "Doanh số (đơn giao xong)"}</th>
            <th>Phí ship thu khách</th>
            <th>Giá trị TB</th>
            {hasCancelled && <th>Hủy</th>}
            <th>Thành công</th>
            <th>Hoàn</th>
            <th>Tỷ lệ thành công</th>
          </tr>
        </thead>
        <tbody>
          {d.rows.map((r) => (
            <tr key={r.key}>
              <td>
                <span className={r.key === "unknown" ? "nm muted" : "nm"} title={r.label}>
                  {r.label}
                </span>
              </td>
              {hasPlaced ? (
                <Num r={r} n={r.orders} text={count(r.orders)} metric="placed" p={p} />
              ) : (
                <td>{count(r.orders)}</td>
              )}
              {hasActive ? (
                <Num r={r} n={r.active ?? 0} text={vnd(r.goodsAmount)} metric="placed_active" money p={p} />
              ) : (
                <Num r={r} n={r.delivered} text={vnd(r.goodsAmount)} metric="delivered" money p={p} />
              )}
              <MoneyCell v={r.shippingFeeCharged} />
              <MoneyCell v={r.avgGoodsAmount} />
              {hasCancelled && (
                <Num r={r} n={r.cancelled ?? 0} text={count(r.cancelled ?? 0)} metric="cancelled" p={p} />
              )}
              <Num r={r} n={r.delivered} text={count(r.delivered)} metric="delivered" p={p} />
              <Num r={r} n={r.returned} text={count(r.returned)} metric="returned" p={p} />
              <td>{rate(r.successRateBp)}</td>
            </tr>
          ))}
          <tr className="is-total">
            <td>Tổng</td>
            <td>{count(d.totals.orders)}</td>
            <MoneyCell v={d.totals.goodsAmount} />
            <MoneyCell v={d.totals.shippingFeeCharged} />
            <MoneyCell v={d.totals.avgGoodsAmount} />
            {hasCancelled && <td>{count(d.totals.cancelled ?? 0)}</td>}
            <td>{count(d.totals.delivered)}</td>
            <td>{count(d.totals.returned)}</td>
            <td>{rate(d.totals.successRateBp)}</td>
          </tr>
        </tbody>
      </table>
    </div>
  );
}

export function OrdersReport() {
  // Đổi bất kỳ thứ gì của báo cáo (kỳ, cơ sở, nhóm, bộ lọc) thì ô đang mở bị bỏ: số của ô cũ không còn khớp bảng mới.
  const rp = useReportPeriod((next) => {
    writeCell(next, null);
  });
  const { sp, setSp, period, bad } = rp;
  const basis = parseBasis(sp.get("tinh"));
  const groupBy: ReportDimension = parseGroup(sp.get("nhom"));
  const filters = parseFilters(sp);
  const parsedCell = parseCell(sp);
  const open = parsedCell !== null && (parsedCell.basis ?? "placed") === basis ? parsedCell : null;

  const lists = useQuery({
    queryKey: queryKey("report.filters"),
    queryFn: () => unwrap(getSalesReportFilters()),
  });
  const params = { from: period.from, to: period.to, basis, groupBy, ...filterQuery(filters) };
  const rep = useReport<SalesOrdersReport>({
    report: "orders",
    params,
    load: () => unwrap(getSalesOrdersReport({ query: params })),
    parse: (v) => (isOrdersReport(v) ? v : null),
    enabled: bad === null,
  });
  const exp = useExportRun({ report: "orders", params, title: `Báo cáo đơn ${periodText(period.from, period.to)}` });

  const update = (change: (next: URLSearchParams) => void) => {
    const next = new URLSearchParams(sp);
    writeCell(next, null);
    change(next);
    setSp(next);
  };
  const onFilters = (f: OrderFilters) => {
    update((n) => {
      writeFilters(n, f);
    });
  };
  const loading = rep.phase === "loading" && bad === null;
  const data = rep.phase === "ready" ? rep.data : null;
  const showPanel = open !== null && data !== null;
  const rowLabel = open?.key === undefined ? undefined : data?.rows.find((r) => r.key === open.key)?.label;

  return (
    <SalesReportPage title="Báo cáo đơn" sub={SUB} rep={rep} exp={exp} periodBad={bad !== null}>
      <div className={showPanel ? "dd is-open" : "dd"}>
        <div className="rep-main stack">
          <ReportPeriodPicker rp={rp} disabled={loading} />
          <div className="rep-f">
            <div className="field">
              <label htmlFor="rp-basis">Tính theo</label>
              <span className="select-wrap">
                <select
                  id="rp-basis"
                  value={basis}
                  onChange={(e) => {
                    update((n) => {
                      n.set("tinh", parseBasis(e.target.value));
                    });
                  }}
                >
                  <option value="placed">Ngày lên đơn</option>
                  <option value="completed">Ngày hoàn tất</option>
                </select>
              </span>
            </div>
            <div className="field">
              <label htmlFor="rp-group">Nhóm theo</label>
              <span className="select-wrap">
                <select
                  id="rp-group"
                  value={groupBy}
                  onChange={(e) => {
                    update((n) => {
                      n.set("nhom", parseGroup(e.target.value));
                    });
                  }}
                >
                  {GROUPS.map((g) => (
                    <option key={g.value} value={g.value}>
                      {g.label}
                    </option>
                  ))}
                </select>
              </span>
            </div>
          </div>
          <MultiFilter value={filters} lists={lists.data} onChange={onFilters} disabled={loading} />
          {lists.isError && <p className="field-err">Không tải được danh sách bộ lọc. Tải lại trang để thử lại.</p>}
          <ReportBanners rep={rep} failedHint="Thu hẹp kỳ hay thêm bộ lọc, rồi bấm Tải lại." />
          {data !== null && rep.updatedAt !== null && (
            <>
              <div className="meta">
                <span>
                  Theo <b>{basis === "completed" ? "ngày hoàn tất" : "ngày lên đơn"}</b>
                  {basis === "completed" ? " (giao thành công hoặc hoàn về kho)" : ""} ·{" "}
                  {periodText(period.from, period.to)}, giờ Việt Nam
                </span>
                <span>
                  Cập nhật lúc <b>{clockTime(rep.updatedAt)}</b>
                </span>
              </div>
              {data.rows.length === 0 ? (
                <div className="empty">
                  <h2>{TEXT_EMPTY}</h2>
                  <p className="muted">Đổi kỳ, cơ sở ngày hoặc bỏ bớt bộ lọc.</p>
                  {hasFilters(filters) && (
                    <button
                      type="button"
                      className="btn btn-quiet"
                      onClick={() => {
                        onFilters(noFilters());
                      }}
                    >
                      Bỏ lọc
                    </button>
                  )}
                </div>
              ) : (
                <OrdersTable
                  d={data}
                  basis={basis}
                  open={open}
                  onOpen={(c) => {
                    const next = new URLSearchParams(sp);
                    writeCell(next, c);
                    setSp(next);
                  }}
                />
              )}
              <p className="xs muted">
                {basis === "completed"
                  ? 'Theo ngày hoàn tất: tập là đơn giao xong hoặc hoàn trong kỳ; số đơn = thành công + hoàn; doanh số chỉ của đơn giao xong; cột Đơn lên, Hủy ẩn. Đơn thiếu kênh vào "Không xác định", không tự gán vào Facebook.'
                  : 'Tỷ lệ thành công = thành công ÷ (thành công + hoàn); chưa có đơn kết thúc thì hiện "—". Giá trị TB = doanh số ÷ đơn lên chưa hủy, làm tròn tới đồng.'}
              </p>
              <p className="small muted">
                Doanh số = tiền hàng sau giảm giá, chưa gồm ship; chưa phải doanh thu kế toán.
              </p>
            </>
          )}
        </div>
        {showPanel && (
          <DrilldownPanel
            cell={open}
            period={period}
            filter={filterQuery(filters)}
            {...(rowLabel === undefined ? {} : { label: rowLabel })}
            filterNote={hasFilters(filters) ? "theo bộ lọc của bảng" : "không lọc thêm"}
            backLabel="Báo cáo đơn"
            onClose={() => {
              const next = new URLSearchParams(sp);
              writeCell(next, null);
              setSp(next, { replace: true });
            }}
          />
        )}
      </div>
    </SalesReportPage>
  );
}
