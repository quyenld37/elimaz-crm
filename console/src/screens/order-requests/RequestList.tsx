// Danh sách Yêu cầu về đơn hàng /don/yeu-cau (kế hoạch M5 Task 207, M5-24; mockup docs/mockups/m5/order-requests.html
// trạng thái A, F; HV03). Máy chủ trả mới nhất trước, lọc một trạng thái, một loại, q (mã yêu cầu, mã đơn, SĐT, tên khách
// không dấu). API chưa trả tên khách, số ảnh, số đếm theo trạng thái: màn không vẽ các cột, số đó (tệp task, Quyết định
// 1). Báo hiệu order_request.changed tải lại danh sách (yêu cầu mới tự hiện).
import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { listOrderRequests, type OrderRequestKind, type OrderRequestPage, type OrderRequestStatus } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { shortTime } from "../../app/time";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import { MoreBar } from "../ma-giam-gia/MoreBar";
import { PAGE, REQUEST_TOPIC, usePerms } from "./requestData";
import { KINDS, KIND_TEXT, STATUSES, STATUS_TEXT, isClosed, reasonText, statusChip } from "./requestRules";
import "../don/don.css";
import "./order-requests.css";

const asKind = (v: string): OrderRequestKind | "" => KINDS.find((k) => k === v) ?? "";

export function RequestList() {
  const perms = usePerms();
  const [status, setStatus] = useState<OrderRequestStatus | "">("");
  const [kind, setKind] = useState<OrderRequestKind | "">("");
  const [text, setText] = useState("");
  const q = useDebounced(text.trim(), SEARCH_DELAY_MS);
  const query = {
    ...(status === "" ? {} : { status }),
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
  const filtered = status !== "" || kind !== "" || q !== "";
  const canOrder = perms.can("order.read");

  const label = (value: OrderRequestStatus | "", name: string) => (
    <button
      key={name}
      type="button"
      className="lbl"
      aria-pressed={status === value}
      onClick={() => {
        setStatus(value);
      }}
    >
      {name}
    </button>
  );
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
            (không cần tải lại).
          </p>
        </div>
      </div>

      <div className="scroll-x">
        <div className="lblbar" role="group" aria-label="Lọc theo trạng thái">
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
          <h2>{filtered ? "Không có yêu cầu nào khớp" : "Chưa có yêu cầu nào"}</h2>
          <p className="muted">Yêu cầu mới từ website sẽ tự hiện ở đây.</p>
          {filtered && (
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
                  {["Yêu cầu", "Đơn", "Lý do", "Trạng thái", "Cập nhật"].map((h) => (
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
                    <td data-label="Lý do">
                      <span className="clamp2 wrap-any">{reasonText(r.reasonCode)}</span>
                    </td>
                    <td data-label="Trạng thái">
                      <span className={statusChip(r.status)}>{STATUS_TEXT[r.status]}</span>
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
