// Danh sách Yêu cầu về đơn hàng /don/yeu-cau (kế hoạch M5 Task 207, M5-24; Task 486; mockup
// docs/mockups/m5/order-requests.html trạng thái A, F; HV03). Máy chủ trả mới nhất trước, lọc trạng thái (một, hay nhãn
// "Đang mở" = các trạng thái chưa đóng — mặc định), một loại, q (mã yêu cầu, mã đơn, SĐT, tên khách không dấu). Số trên
// nhãn đếm cả shop (countOrderRequests), không theo loại, ô tìm. Báo hiệu order_request.changed tải lại danh sách và
// số (yêu cầu mới tự hiện).
import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import {
  listOrderRequests,
  type OrderRequestKind,
  type OrderRequestPage,
  type OrderRequestStatus,
  type OrderRequestSummary,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { shortTime, VN_TIME_ZONE } from "../../app/time";
import { CountLabel } from "../don/CountLabel";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import { MoreBar } from "../ma-giam-gia/MoreBar";
import { PAGE, REQUEST_TOPIC, usePerms, useRequestCounts } from "./requestData";
import { KINDS, KIND_TEXT, OPEN_STATUSES, STATUSES, STATUS_TEXT, isClosed, statusChip } from "./requestRules";
import "../don/don.css";
import "./order-requests.css";

const asKind = (v: string): OrderRequestKind | "" => KINDS.find((k) => k === v) ?? "";

/** Bộ lọc trạng thái của thanh nhãn: "open" là nhãn "Đang mở", "" là "Tất cả". */
type StatusFilter = OrderRequestStatus | "open" | "";

const dayMonth = new Intl.DateTimeFormat("en-GB", { day: "2-digit", month: "2-digit", timeZone: VN_TIME_ZONE });

/** Chữ trạng thái của dòng; đã xác nhận đổi kèm hạn gửi hàng về (mockup A: "Đã xác nhận đổi · gửi trước 25/11"). */
function statusLine(r: OrderRequestSummary): string {
  const text = STATUS_TEXT[r.status];
  if (r.status !== "confirmed" || r.shipBackDeadline === undefined) return text;
  return `${text} · gửi trước ${dayMonth.format(new Date(r.shipBackDeadline))}`;
}

/** Số ảnh: đổi, trả hiện số (cả 0); hủy, hỏi không gửi ảnh → "—". */
const photos = (r: OrderRequestSummary): string =>
  r.kind === "exchange" || r.kind === "return" ? String(r.photoCount) : "—";

export function RequestList() {
  const perms = usePerms();
  const [status, setStatus] = useState<StatusFilter>("open");
  const [kind, setKind] = useState<OrderRequestKind | "">("");
  const [text, setText] = useState("");
  const q = useDebounced(text.trim(), SEARCH_DELAY_MS);
  const query = {
    ...(status === "open" ? { statuses: [...OPEN_STATUSES] } : status === "" ? {} : { status }),
    ...(kind === "" ? {} : { kind }),
    ...(q === "" ? {} : { q }),
    limit: PAGE,
  };
  const list = useInfiniteQuery({
    queryKey: queryKey(REQUEST_TOPIC, "list", query),
    // "" là trang đầu (chưa có con trỏ).
    initialPageParam: "",
    getNextPageParam: (last: OrderRequestPage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(listOrderRequests({ query: { ...query, ...(pageParam === "" ? {} : { cursor: pageParam }) } })),
  });
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  const counts = useRequestCounts();
  const filtered = (status !== "" && status !== "open") || kind !== "" || q !== "";
  const canOrder = perms.can("order.read");

  const label = (value: StatusFilter, name: string) => {
    const c = counts.data;
    const n =
      c === undefined
        ? undefined
        : value === "open"
          ? c.open
          : value === ""
            ? c.total
            : c.statuses.find((x) => x.status === value)?.count;
    return (
      <CountLabel
        key={name}
        text={name}
        n={n}
        pressed={status === value}
        attention={value === "open"}
        onSelect={() => {
          setStatus(value);
        }}
      />
    );
  };
  const showAll = () => {
    setStatus("");
    setKind("");
    setText("");
  };

  return (
    <div>
      <div className="page-h">
        <div className="grow">
          <h1>Yêu cầu về đơn hàng</h1>
          <p className="sub">
            Khách gửi từ trang đơn hàng trên website: đổi hàng, trả hàng, hủy đơn, hỏi về đơn. Yêu cầu mới tự hiện
            (không cần tải lại); số cạnh mục menu là số yêu cầu đang mở.
          </p>
        </div>
      </div>

      <div className="scroll-x">
        <div className="lblbar" role="group" aria-label="Lọc theo trạng thái">
          {label("open", "Đang mở")}
          {STATUSES.map((s) => label(s, STATUS_TEXT[s]))}
          {label("", "Tất cả")}
        </div>
      </div>
      <div className="toolbar">
        <input
          className="input is-search"
          type="search"
          placeholder="Tìm mã yêu cầu, mã đơn, tên khách (gõ không dấu cũng được)"
          aria-label="Tìm yêu cầu"
          maxLength={100}
          value={text}
          onChange={(e) => {
            setText(e.target.value);
          }}
        />
        <div className="select-wrap">
          <select
            aria-label="Loại yêu cầu"
            value={kind}
            onChange={(e) => {
              setKind(asKind(e.target.value));
            }}
          >
            <option value="">Mọi loại</option>
            {KINDS.map((k) => (
              <option key={k} value={k}>
                {KIND_TEXT[k]}
              </option>
            ))}
          </select>
        </div>
      </div>

      {list.isError ? (
        <div className="box box-err stack-sm">
          <b>Chưa tải được danh sách yêu cầu.</b>
          <p>{errorMessage(list.error)}</p>
          <div>
            <button
              type="button"
              className="btn btn-line"
              onClick={() => {
                void list.refetch();
              }}
            >
              Thử lại
            </button>
          </div>
        </div>
      ) : list.isPending ? (
        <p className="muted">Đang tải danh sách yêu cầu…</p>
      ) : rows.length === 0 ? (
        <div className="empty">
          <h2>
            {filtered
              ? "Không có yêu cầu nào khớp"
              : status === "open"
                ? "Không có yêu cầu đang mở"
                : "Chưa có yêu cầu nào"}
          </h2>
          <p className="muted">Yêu cầu mới từ website sẽ tự hiện ở đây.</p>
          {(filtered || status === "open") && (
            <button type="button" className="btn btn-quiet" onClick={showAll}>
              Xem tất cả yêu cầu
            </button>
          )}
        </div>
      ) : (
        <>
          <div className="tbl-frame">
            <table className="tbl rq">
              <thead>
                <tr>
                  {["Yêu cầu", "Đơn", "Khách", "Trạng thái", "Ảnh", "Cập nhật"].map((h) => (
                    <th key={h}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.id} className={isClosed(r.status) ? "is-muted" : undefined}>
                    <td className="lead">
                      <div className="who">
                        <Link className="cell-a bold num" to={`/don/yeu-cau/${r.id}`}>
                          {r.ref}
                        </Link>
                        <span className="small">{KIND_TEXT[r.kind]}</span>
                      </div>
                    </td>
                    <td data-label="Đơn">
                      {canOrder ? (
                        <Link className="cell-a num" to={`/don/${r.orderId}`}>
                          {r.orderCode}
                        </Link>
                      ) : (
                        <span className="num">{r.orderCode}</span>
                      )}
                    </td>
                    <td data-label="Khách">
                      <span className="clamp2 wrap-any">{r.customerName ?? "—"}</span>
                    </td>
                    <td data-label="Trạng thái">
                      <span className={statusChip(r.status)}>{statusLine(r)}</span>
                    </td>
                    <td data-label="Ảnh" className="num">
                      {photos(r)}
                    </td>
                    <td data-label="Cập nhật">
                      <span className="small num">{shortTime(r.updatedAt)}</span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <MoreBar text={`${String(rows.length)} yêu cầu${list.hasNextPage ? ", còn nữa" : ""}`} pages={list} />
        </>
      )}
    </div>
  );
}
