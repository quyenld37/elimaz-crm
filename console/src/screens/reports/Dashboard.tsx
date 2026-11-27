// Màn Tổng quan /bao-cao/tong-quan (Task 239, M6-16; mockup docs/m6/mockup/tong-quan.html trạng thái A–H; BC01, BC05).
// Bảy ô số theo kỳ, khối "Chờ xử lý" (số hiện tại, không theo kỳ), bảng theo kênh, bảng theo ngày khi kỳ dài hơn một
// ngày (thanh ngang bằng CSS, Q15). Mọi số do máy chủ tính (kể cả dòng Tổng); màn chỉ định dạng. Bấm ô số mở danh sách
// chi tiết (ngăn bên phải; điện thoại: trang riêng). Tự tải lại mỗi 60 s khi tab đang hiện (Q21).
import { createContext, useContext, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router";
import { getSalesDashboard, type ReportAmounts, type SalesDashboardReport } from "../../api";
import { unwrap } from "../../app/errors";
import { DrilldownPanel } from "./DrilldownPanel";
import { ExportButton, ExportNotice, EXPORTS_PATH } from "./ExportButton";
import { PeriodPicker } from "./PeriodPicker";
import { ReportsPage } from "./ReportsLayout";
import { RunNotice } from "./RunNotice";
import { channelName, parseCell, sameCell, writeCell, type Cell } from "./cells";
import { isDashboard } from "./dashboardData";
import { clockTime, count, dayLabel, periodText, vnd } from "./format";
import { periodError, parsePeriod, vnToday, weekday, writePeriod, type Period } from "./period";
import { useExportRun } from "./useRunNotice";
import { useReport } from "./useReport";

export const REFRESH_MS = 60_000;
export const TEXT_BACKGROUND = "Báo cáo nặng, đang chạy nền. Sẽ báo khi xong.";

const SUB =
  "Số đơn và doanh số của shop theo kỳ, cùng số đơn đang chờ xử lý lúc này. Bấm một ô số để xem danh sách đơn tạo nên số đó.";

const PLACED: Cell = { metric: "placed" };
const ACTIVE: Cell = { metric: "placed_active" };

function emptyText(p: Period): string {
  if (p.preset === "today") return "Chưa có đơn nào lên hôm nay";
  if (p.preset === "yesterday") return "Chưa có đơn nào lên hôm qua";
  return "Chưa có đơn nào lên trong kỳ này";
}

/** Ô đang mở và cách mở một ô (địa chỉ cho ô dạng liên kết, hàm cho ô dạng nút), cấp cho mọi ô số của bản tổng quan. */
interface CellActions {
  open: Cell | null;
  href: (c: Cell) => string;
  onOpen: (c: Cell) => void;
}

const CellContext = createContext<CellActions>({ open: null, href: () => "", onOpen: () => undefined });

interface KpiProps {
  label: string;
  value: string;
  money?: boolean;
  sub?: ReactNode;
  cell: Cell | null;
}

/** Một ô số: liên kết mở danh sách chi tiết khi có đơn; ô 0 đơn là khối tĩnh (không có gì để mở). */
function Kpi({ label, value, money = false, sub, cell }: KpiProps) {
  const { open, href } = useContext(CellContext);
  const body = (
    <>
      <span className="k">{label}</span>
      <span className={money ? "v money" : "v"}>{value}</span>
      {sub !== undefined && <span className="s">{sub}</span>}
    </>
  );
  if (cell === null) return <div className="kpi">{body}</div>;
  return (
    <Link className="kpi" to={href(cell)} aria-current={sameCell(cell, open) ? "true" : undefined}>
      {body}
    </Link>
  );
}

interface CellButtonProps {
  value: string;
  money?: boolean;
  cell: Cell;
}

function CellButton({ value, money = false, cell }: CellButtonProps) {
  const { open, onOpen } = useContext(CellContext);
  return (
    <button
      type="button"
      className={money ? "cell-n money" : "cell-n"}
      aria-pressed={sameCell(cell, open)}
      onClick={() => {
        onOpen(cell);
      }}
    >
      {value}
    </button>
  );
}

const orNone = (n: number, cell: Cell) => (n > 0 ? cell : null);

/** Một ô tiền chỉ để xem (không mở được). */
function MoneyTd({ v }: { v: number }) {
  return (
    <td>
      <span className="money">{vnd(v)}</span>
    </td>
  );
}

/** Hai ô liền nhau của một dòng bảng: số đơn và tiền hàng của cùng một ô số (cùng danh sách chi tiết). */
function CountMoneyTds({ orders, goods, cell }: { orders: number; goods: number; cell: Cell }) {
  return (
    <>
      <td>
        <NumCell n={orders} text={count(orders)} cell={cell} />
      </td>
      <td>
        <NumCell n={orders} text={vnd(goods)} money cell={cell} />
      </td>
    </>
  );
}

/** Ô số của bảng: nút mở danh sách khi có đơn, chữ tĩnh khi 0 đơn. */
function NumCell({ n, text, money = false, cell }: { n: number; text: string; money?: boolean; cell: Cell }) {
  if (n > 0) return <CellButton value={text} money={money} cell={cell} />;
  return money ? <span className="money">{text}</span> : text;
}

interface BodyProps {
  d: SalesDashboardReport;
  period: Period;
  at: Date;
}

function Overview({ d, period, at }: BodyProps) {
  const completed: Cell["basis"] = "completed";
  const money = (a: ReportAmounts) => <span className="money">{vnd(a.goodsAmount)}</span>;
  const maxDay = Math.max(0, ...d.byDay.map((r) => r.active.goodsAmount));
  return (
    <>
      <div className="meta">
        <span>
          Đơn lên, doanh số, hủy theo <b>ngày lên đơn</b>; thành công, hoàn theo <b>ngày hoàn tất</b>; hàng khách trả
          theo <b>ngày nhận trả</b> · {periodText(period.from, period.to)}, giờ Việt Nam
        </span>
        <span>
          Cập nhật lúc <b>{clockTime(at)}</b> · tự tải lại mỗi 60 giây
        </span>
      </div>

      <div className="kpis is-6">
        <Kpi
          label="Đơn lên"
          value={count(d.placed.orders)}
          sub={`gồm ${count(d.cancelled.orders)} đơn đã hủy`}
          cell={orNone(d.placed.orders, PLACED)}
        />
        <Kpi
          label="Doanh số"
          value={vnd(d.active.goodsAmount)}
          money
          sub={`${count(d.active.orders)} đơn chưa hủy`}
          cell={orNone(d.active.orders, ACTIVE)}
        />
        <Kpi
          label="Giá trị TB"
          value={d.avgGoodsAmount === null ? "—" : vnd(d.avgGoodsAmount)}
          money
          sub={d.avgGoodsAmount === null ? undefined : `doanh số ÷ ${count(d.active.orders)} đơn`}
          cell={orNone(d.active.orders, ACTIVE)}
        />
        <Kpi
          label="Đơn hủy"
          value={count(d.cancelled.orders)}
          sub="theo ngày lên đơn"
          cell={orNone(d.cancelled.orders, { metric: "cancelled" })}
        />
        <Kpi
          label="Thành công"
          value={count(d.delivered.orders)}
          sub={<>{money(d.delivered)} · theo ngày hoàn tất</>}
          cell={orNone(d.delivered.orders, { metric: "delivered", basis: completed })}
        />
        <Kpi
          label="Hoàn"
          value={count(d.returned.orders)}
          sub={<>{money(d.returned)} · theo ngày hoàn tất</>}
          cell={orNone(d.returned.orders, { metric: "returned", basis: completed })}
        />
        <Kpi
          label="Hàng khách trả"
          value={`${count(d.customerReturns.items)} cái`}
          sub={
            <>
              <span className="money">{vnd(d.customerReturns.refundAmount)}</span> hoàn tiền · theo ngày nhận trả
            </>
          }
          cell={orNone(d.customerReturns.items, { metric: "customer_returned" })}
        />
      </div>

      <div className="now">
        <div className="row-between">
          <h3>Chờ xử lý</h3>
          <span className="xs muted">Số hiện tại lúc {clockTime(at)}, không theo kỳ đã chọn</span>
        </div>
        <div className="kpis">
          <Kpi
            label="Chờ xác nhận"
            value={count(d.pending.confirm)}
            cell={orNone(d.pending.confirm, { metric: "pending_confirm" })}
          />
          <Kpi
            label="Chờ gửi hãng"
            value={count(d.pending.ship)}
            cell={orNone(d.pending.ship, { metric: "pending_ship" })}
          />
          <Kpi
            label="Đang yêu cầu hủy"
            value={count(d.pending.cancelRequested)}
            cell={orNone(d.pending.cancelRequested, { metric: "cancel_requested" })}
          />
        </div>
      </div>

      {d.placed.orders === 0 && (
        <div className="empty">
          <h2>{emptyText(period)}</h2>
          <p className="muted">Đơn nháp chưa chốt không tính. Chọn kỳ khác để xem số cũ.</p>
        </div>
      )}

      {d.byChannel.length > 0 && (
        <div className="stack-sm">
          <h3>Theo kênh</h3>
          <div className="rt-frame scroll-x">
            <table className="rt" aria-label="Theo kênh">
              <thead>
                <tr>
                  <th>Kênh</th>
                  <th>Đơn lên</th>
                  <th>Chưa hủy</th>
                  <th>Doanh số</th>
                  <th>Phí ship thu khách</th>
                </tr>
              </thead>
              <tbody>
                {d.byChannel.map((r) => {
                  const placed: Cell = { metric: "placed", dimension: "channel", key: r.channel };
                  const active: Cell = { metric: "placed_active", dimension: "channel", key: r.channel };
                  return (
                    <tr key={r.channel}>
                      <td>{channelName(r.channel)}</td>
                      <td>
                        <NumCell n={r.placed.orders} text={count(r.placed.orders)} cell={placed} />
                      </td>
                      <CountMoneyTds orders={r.active.orders} goods={r.active.goodsAmount} cell={active} />
                      <MoneyTd v={r.active.shippingFeeCharged} />
                    </tr>
                  );
                })}
                <tr className="is-total">
                  <td>Tổng</td>
                  <td>{count(d.placed.orders)}</td>
                  <td>{count(d.active.orders)}</td>
                  <MoneyTd v={d.active.goodsAmount} />
                  <MoneyTd v={d.active.shippingFeeCharged} />
                </tr>
              </tbody>
            </table>
          </div>
          {d.byChannel.some((r) => r.channel === "unknown") && (
            <p className="xs muted">
              &quot;Không xác định&quot; là đơn thiếu kênh (dữ liệu cũ, lỗi nhập); không tự gán vào Facebook.
            </p>
          )}
        </div>
      )}
      <p className="small muted">Doanh số = tiền hàng sau giảm giá, chưa gồm ship; chưa phải doanh thu kế toán.</p>

      {d.byDay.length > 0 && (
        <div className="stack-sm">
          <h3>Theo ngày</h3>
          <div className="rt-frame scroll-x">
            <table className="rt" aria-label="Theo ngày">
              <thead>
                <tr>
                  <th>Ngày</th>
                  <th className="l">Doanh số</th>
                  <th>Chưa hủy</th>
                  <th>Doanh số</th>
                  <th>Thành công</th>
                  <th>Doanh số thành công</th>
                </tr>
              </thead>
              <tbody>
                {d.byDay.map((r) => {
                  const pct = maxDay === 0 ? 0 : Math.round((r.active.goodsAmount * 100) / maxDay);
                  const base = { dimension: "day", key: r.day } as const;
                  const active: Cell = { metric: "placed_active", ...base };
                  const done: Cell = { metric: "delivered", basis: completed, ...base };
                  return (
                    <tr key={r.day}>
                      <td>
                        <span>{dayLabel(r.day)}</span> <span className="muted">{weekday(r.day)}</span>
                      </td>
                      <td className="l">
                        <div className="bar is-day">
                          <i style={{ width: `${String(pct)}%` }} />
                        </div>
                      </td>
                      <CountMoneyTds orders={r.active.orders} goods={r.active.goodsAmount} cell={active} />
                      <CountMoneyTds orders={r.delivered.orders} goods={r.delivered.goodsAmount} cell={done} />
                    </tr>
                  );
                })}
                <tr className="is-total">
                  <td>Tổng</td>
                  <td className="l" />
                  <td>{count(d.active.orders)}</td>
                  <MoneyTd v={d.active.goodsAmount} />
                  <td>{count(d.delivered.orders)}</td>
                  <MoneyTd v={d.delivered.goodsAmount} />
                </tr>
              </tbody>
            </table>
          </div>
          <p className="xs muted">
            Đơn chưa hủy, doanh số theo ngày lên đơn; thành công theo ngày hoàn tất. Thanh dài nhất = ngày có doanh số
            lớn nhất của kỳ.
          </p>
        </div>
      )}
    </>
  );
}

export function Dashboard() {
  const [sp, setSp] = useSearchParams();
  const today = vnToday(new Date());
  const period = parsePeriod(sp, today);
  const bad = periodError(period, today);
  const open = parseCell(sp);
  const params = { from: period.from, to: period.to };

  const rep = useReport<SalesDashboardReport>({
    report: "dashboard",
    params,
    load: () => unwrap(getSalesDashboard({ query: params })),
    parse: (v) => (isDashboard(v) ? v : null),
    refreshMs: REFRESH_MS,
    enabled: bad === null,
  });
  const exp = useExportRun({ report: "dashboard", params, title: `Tổng quan ${periodText(period.from, period.to)}` });

  const withCell = (c: Cell | null) => {
    const next = new URLSearchParams(sp);
    writeCell(next, c);
    return next;
  };
  const href = (c: Cell) => `?${withCell(c).toString()}`;
  const actions: CellActions = {
    open,
    href,
    onOpen: (c) => {
      setSp(withCell(c));
    },
  };
  const loading = rep.phase === "loading" && bad === null;

  const onPeriod = (p: Period) => {
    const next = withCell(null);
    writePeriod(next, p);
    setSp(next);
  };

  const showPanel = open !== null && rep.phase === "ready";
  return (
    <ReportsPage
      title="Tổng quan"
      sub={SUB}
      salesScope
      actions={
        <>
          <button
            type="button"
            className="btn btn-quiet"
            disabled={loading || bad !== null || rep.phase === "background"}
            onClick={rep.reload}
          >
            Tải lại
          </button>
          <ExportButton exp={exp} disabled={loading || bad !== null} />
        </>
      }
    >
      <ExportNotice exp={exp} />
      <div className={showPanel ? "dd is-open" : "dd"}>
        <div className="rep-main stack">
          <PeriodPicker
            key={`${period.preset}|${period.from}|${period.to}`}
            value={period}
            today={today}
            onChange={onPeriod}
            disabled={loading}
          />
          {loading && (
            <div className="box box-info stack-sm">
              <b>Đang tải số liệu…</b>
            </div>
          )}
          {rep.phase === "error" && (
            <div className="box box-err stack-sm">
              <b>Không tải được báo cáo.</b>
              <p className="small">{rep.message}</p>
              <div className="row">
                <button type="button" className="btn btn-line" onClick={rep.reload}>
                  Thử lại
                </button>
              </div>
            </div>
          )}
          {rep.phase === "background" && (
            <>
              <RunNotice
                text={TEXT_BACKGROUND}
                action={
                  <Link className="btn btn-line" to={EXPORTS_PATH}>
                    Tệp xuất
                  </Link>
                }
              />
              {rep.message !== null && (
                <div className="box box-err stack-sm">
                  <b>{rep.message}</b>
                  <div className="row">
                    <button type="button" className="btn btn-line" onClick={rep.retryRun}>
                      Thử lại
                    </button>
                  </div>
                </div>
              )}
            </>
          )}
          {rep.phase === "failed" && (
            <div className="box box-err stack-sm">
              <b>{rep.message}</b>
              <p className="small">Thử kỳ ngắn hơn (ví dụ một tháng), rồi bấm Tải lại.</p>
            </div>
          )}
          {rep.phase === "ready" && rep.data !== null && rep.updatedAt !== null && (
            <CellContext value={actions}>
              <Overview d={rep.data} period={period} at={rep.updatedAt} />
            </CellContext>
          )}
        </div>
        {showPanel && (
          <DrilldownPanel
            cell={open}
            period={period}
            onClose={() => {
              setSp(withCell(null), { replace: true });
            }}
          />
        )}
      </div>
    </ReportsPage>
  );
}
