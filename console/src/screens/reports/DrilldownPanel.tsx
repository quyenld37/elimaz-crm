// Danh sách chi tiết của một ô số (Task 239, M6-16; hành vi (a); mockup tong-quan.html trạng thái D, E). Ngăn bên phải
// trên máy tính, trang riêng có nút "← quay lại" trên điện thoại (CSS .dd; ô nằm trên địa chỉ nên nút quay lại của trình
// duyệt cũng đúng). Đầu bảng "N đơn" = `total` của máy chủ (không đếm lại ở màn). Chỉ hiện mã đơn, mã khách, trạng
// thái, kênh, người phụ trách, tiền; API không trả tên, SĐT, địa chỉ khách. Bấm mã đơn mở chi tiết đơn của M2.
import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { listSalesOrdersDrilldown, type SalesOrdersDrilldown, type SalesOrdersDrilldownItem } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { shortTime } from "../../app/time";
import { LABEL_TEXT } from "../don/labels";
import { MoreBar } from "../ma-giam-gia/MoreBar";
import { basisText, cellTitle, channelName, type Cell } from "./cells";
import { count, periodText, vnd } from "./format";

export const PAGE_SIZE = 200;
export const TEXT_DRILL_BUSY = "Danh sách đang bận hoặc quá nặng. Thu hẹp kỳ rồi thử lại.";
export const TEXT_DRILL_EMPTY = "Không có đơn nào trong ô này.";

/** Bộ lọc của báo cáo đơn (Task 240) gửi kèm: cùng tham số với getSalesOrdersReport. */
export interface DrilldownFilter {
  channel?: string[];
  sourceId?: string[];
  careBy?: string[];
  createdBy?: string[];
  pageId?: string[];
  carrierKey?: string[];
}

export interface DrilldownPanelProps {
  cell: Cell;
  period: { from: string; to: string };
  filter?: DrilldownFilter;
  onClose: () => void;
  /** Chữ của nút quay lại trên điện thoại: "← Tổng quan". */
  backLabel?: string;
}

type PageResult = SalesOrdersDrilldown | "busy";

function Row({ r, completed }: { r: SalesOrdersDrilldownItem; completed: boolean }) {
  const at = completed ? (r.completedAt ?? r.placedAt) : r.placedAt;
  return (
    <tr>
      <td>
        <Link className="cell-a" to={`/don/${encodeURIComponent(r.orderId)}`}>
          {r.code}
        </Link>
      </td>
      <td className="l">{r.customerCode ?? "—"}</td>
      <td className="l">{at === undefined ? "—" : shortTime(at)}</td>
      <td className="l">{LABEL_TEXT[r.statusLabel]}</td>
      <td className="l">{channelName(r.channel)}</td>
      <td className="l">
        {r.careByName === undefined || r.careByName === "" ? (
          <span className="nm muted">Chưa phân công</span>
        ) : (
          <span className="nm" title={r.careByName}>
            {r.careByName}
          </span>
        )}
      </td>
      <td>
        <span className="money">{vnd(r.goodsAmount)}</span>
      </td>
      <td>
        <span className="money">{vnd(r.shippingFeeCharged)}</span>
      </td>
    </tr>
  );
}

export function DrilldownPanel({ cell, period, filter, onClose, backLabel = "Tổng quan" }: DrilldownPanelProps) {
  const completed = cell.basis === "completed";
  const query = {
    from: period.from,
    to: period.to,
    metric: cell.metric,
    limit: PAGE_SIZE,
    ...(cell.basis === undefined ? {} : { basis: cell.basis }),
    ...(cell.dimension !== undefined && cell.key !== undefined
      ? { cellDimension: cell.dimension, cellKey: cell.key }
      : {}),
    ...filter,
  };
  const q = useInfiniteQuery({
    queryKey: queryKey("report.drilldown", query),
    initialPageParam: "",
    queryFn: async ({ pageParam }): Promise<PageResult> => {
      const r = await unwrap(
        listSalesOrdersDrilldown({ query: { ...query, ...(pageParam === "" ? {} : { cursor: pageParam }) } }),
      );
      return r.status === "ready" ? r : "busy";
    },
    getNextPageParam: (last) => (last === "busy" ? undefined : last.nextCursor),
  });

  const first = q.data?.pages[0];
  const pages = q.data?.pages.filter((p): p is SalesOrdersDrilldown => p !== "busy") ?? [];
  const items = pages.flatMap((p) => p.items);
  const total = pages[0]?.total;

  return (
    <aside className="dd-panel" aria-label="Danh sách chi tiết">
      <button type="button" className="btn btn-quiet dd-back" onClick={onClose}>
        ← {backLabel}
      </button>
      <div className="dd-h">
        <div className="grow">
          <h3>{total === undefined ? cellTitle(cell) : `${cellTitle(cell)} · ${count(total)} đơn`}</h3>
          <p className="xs muted">
            {periodText(period.from, period.to)} · {basisText(cell)}
          </p>
        </div>
        <button type="button" className="ibtn dd-close" aria-label="Đóng" onClick={onClose}>
          ✕
        </button>
      </div>
      {q.isPending && <p className="muted">Đang tải danh sách…</p>}
      {q.isError && (
        <div className="box box-err stack-sm">
          <b>Không tải được danh sách.</b>
          <p className="small">{errorMessage(q.error)}</p>
          <div className="row">
            <button
              type="button"
              className="btn btn-line"
              onClick={() => {
                void q.refetch();
              }}
            >
              Thử lại
            </button>
          </div>
        </div>
      )}
      {first === "busy" && <p className="box box-info small">{TEXT_DRILL_BUSY}</p>}
      {total !== undefined && items.length === 0 && <p className="muted">{TEXT_DRILL_EMPTY}</p>}
      {items.length > 0 && (
        <div className="rt-frame scroll-x">
          <table className="rt">
            <thead>
              <tr>
                <th>Mã đơn</th>
                <th className="l">Mã khách</th>
                <th className="l">{completed ? "Hoàn tất lúc" : "Lên lúc"}</th>
                <th className="l">Trạng thái</th>
                <th className="l">Kênh</th>
                <th className="l">Phụ trách</th>
                <th>Tiền hàng</th>
                <th>Ship</th>
              </tr>
            </thead>
            <tbody>
              {items.map((r) => (
                <Row key={r.orderId} r={r} completed={completed} />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {total !== undefined && items.length > 0 && (
        <MoreBar
          text={`Đang hiện ${count(items.length)} / ${count(total)} đơn`}
          pages={{
            hasNextPage: q.hasNextPage,
            isFetchingNextPage: q.isFetchingNextPage,
            fetchNextPage: () => q.fetchNextPage(),
          }}
          label="Xem thêm"
        />
      )}
      <p className="xs muted">
        Chỉ hiện mã đơn và mã khách, không tên, SĐT, địa chỉ khách. Bấm mã đơn mở màn chi tiết đơn.
      </p>
    </aside>
  );
}
