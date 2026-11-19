// Danh sách mã giảm giá /ma-giam-gia (kế hoạch M2 Task M2-30; mockup docs/mockups/m2/ma-giam-gia.html trạng thái A–C).
// Máy chủ trả mã mới nhất trước, lọc theo q (mã hoặc tên), status (active/disabled), batchId; hết hạn, hết lượt, chưa
// tới ngày là trạng thái hiện suy từ hạn và lượt. API chưa có danh sách đợt mã: ô "Đợt mã" chỉ có các đợt đang thấy
// trên trang (mã thuộc đợt) và đợt vừa tạo.
import { useInfiniteQuery, useQueries } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { Link } from "react-router";
import {
  getCouponBatch,
  listCoupons,
  type Coupon,
  type CouponBatch,
  type CouponPage,
  type CouponStatus,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import { BatchDialog, BatchProgress, useBatchJob } from "./BatchForm";
import { batchKey, COUPON_TOPIC, PAGE, usePerms } from "./couponData";
import { CreateCouponDialog } from "./CouponForm";
import { MoreBar } from "./MoreBar";
import { STATE_CHIP, conditionText, couponState, discountText, isMuted, periodText, usesText } from "./couponRules";
import "./ma-giam-gia.css";

const NUM = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 0 });
type Open = "create" | "batch" | null;

function Row({ c, now, batchName }: { c: Coupon; now: number; batchName: string | undefined }) {
  const state = couponState(c, now);
  const chip = STATE_CHIP[state];
  return (
    <tr className={isMuted(state) ? "is-muted" : undefined}>
      <td className="lead">
        <div className="c-code">
          <Link className="cell-a bold num" to={`/ma-giam-gia/${c.id}`}>
            {c.code}
          </Link>
          <span className="small muted ellip">{batchName ?? c.name}</span>
        </div>
      </td>
      <td data-label="Giảm">{discountText(c)}</td>
      <td data-label="Điều kiện">{conditionText(c)}</td>
      <td data-label="Hạn" className="num">
        {periodText(c)}
      </td>
      <td data-label="Đã dùng" className="r num">
        {usesText(c)}
      </td>
      <td data-label="Trạng thái">
        <span className={chip.className}>{chip.label}</span>
      </td>
    </tr>
  );
}

const batchLabel = (b: CouponBatch | undefined) =>
  b === undefined ? undefined : `Đợt: ${b.name} (${NUM.format(b.quantity)} mã)`;

/** Bảng mã (trạng thái A); children là hàng "Tải thêm" dưới bảng. */
function CouponTable(o: { rows: Coupon[]; now: number; batchOf: Map<string, CouponBatch>; children: ReactNode }) {
  return (
    <>
      <div className="tbl-frame">
        <table className="tbl">
          <thead>
            <tr>
              {["Mã", "Giảm", "Điều kiện", "Hạn"].map((h) => (
                <th key={h}>{h}</th>
              ))}
              <th className="r">Đã dùng</th>
              <th>Trạng thái</th>
            </tr>
          </thead>
          <tbody>
            {o.rows.map((c) => (
              <Row
                key={c.id}
                c={c}
                now={o.now}
                batchName={batchLabel(c.batchId === undefined ? undefined : o.batchOf.get(c.batchId))}
              />
            ))}
          </tbody>
        </table>
      </div>
      {o.children}
    </>
  );
}

export function CouponList() {
  const perms = usePerms();
  const job = useBatchJob();
  const [text, setText] = useState("");
  const [status, setStatus] = useState<CouponStatus | "">("");
  const [batchId, setBatchId] = useState("");
  const [open, setOpen] = useState<Open>(null);
  const [now] = useState(() => Date.now());
  const q = useDebounced(text.trim(), SEARCH_DELAY_MS);

  const query = {
    ...(q === "" ? {} : { q }),
    ...(status === "" ? {} : { status }),
    ...(batchId === "" ? {} : { batchId }),
    limit: PAGE,
  };
  const list = useInfiniteQuery({
    queryKey: queryKey(COUPON_TOPIC, "list", query),
    initialPageParam: "",
    getNextPageParam: (last: CouponPage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(listCoupons({ query: { ...query, ...(pageParam === "" ? {} : { cursor: pageParam }) } })),
  });
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];

  // Đợt đang thấy: đợt của các dòng, đợt đang lọc, đợt vừa tạo (mỗi đợt một lần đọc getCouponBatch).
  const ids = [
    ...new Set([
      ...rows.flatMap((c) => (c.batchId === undefined ? [] : [c.batchId])),
      ...(batchId === "" ? [] : [batchId]),
      ...(job.done === null ? [] : [job.done.id]),
    ]),
  ];
  const batches = useQueries({
    queries: ids.map((id) => ({
      queryKey: batchKey(id),
      queryFn: () => unwrap(getCouponBatch({ path: { batchId: id } })),
    })),
  });
  const batchOf = new Map<string, CouponBatch>();
  for (const b of batches) if (b.data !== undefined) batchOf.set(b.data.id, b.data);
  const canManage = perms.can("promotion.manage");
  const filtered = q !== "" || status !== "" || batchId !== "";

  const actions = canManage && (
    <>
      <button
        type="button"
        className="btn btn-line"
        onClick={() => {
          setOpen("batch");
        }}
      >
        Tạo đợt mã
      </button>
      <button
        type="button"
        className="btn btn-dark"
        onClick={() => {
          setOpen("create");
        }}
      >
        Tạo mã
      </button>
    </>
  );

  return (
    <div>
      <div className="page-h">
        <div>
          <h1>Mã giảm giá</h1>
          <p className="sub">
            Mỗi đơn dùng tối đa một mã. Lượt dùng trừ khi tạo đơn, trả lại khi đơn bị hủy trước khi giao.
          </p>
        </div>
        <div className="row">{actions}</div>
      </div>

      <BatchProgress job={job} onShow={setBatchId} />

      <div className="toolbar">
        <input
          className="input is-search"
          type="search"
          placeholder="Tìm theo mã hoặc tên"
          aria-label="Tìm mã"
          maxLength={64}
          value={text}
          onChange={(e) => {
            setText(e.target.value);
          }}
        />
        <div className="select-wrap">
          <select
            aria-label="Trạng thái"
            value={status}
            onChange={(e) => {
              const v = e.target.value;
              setStatus(v === "active" || v === "disabled" ? v : "");
            }}
          >
            <option value="">Mọi trạng thái</option>
            <option value="active">Đang bật</option>
            <option value="disabled">Đã tắt</option>
          </select>
        </div>
        <div className="select-wrap">
          <select
            aria-label="Đợt mã"
            value={batchId}
            onChange={(e) => {
              setBatchId(e.target.value);
            }}
          >
            <option value="">Mọi đợt</option>
            {[...batchOf.values()].map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </select>
        </div>
      </div>

      {list.isError ? (
        <div className="box box-err">{errorMessage(list.error)}</div>
      ) : list.isSuccess && rows.length === 0 ? (
        <div className="empty">
          <h2>{filtered ? "Không có mã nào khớp" : "Chưa có mã giảm giá nào"}</h2>
          {!filtered && <div className="row">{actions}</div>}
        </div>
      ) : (
        rows.length > 0 && (
          <CouponTable rows={rows} now={now} batchOf={batchOf}>
            <MoreBar text={`${String(rows.length)} ${filtered ? "mã khớp" : "mã mới nhất"}`} pages={list} />
          </CouponTable>
        )
      )}

      {open === "create" && (
        <CreateCouponDialog
          onClose={() => {
            setOpen(null);
          }}
        />
      )}
      {open === "batch" && (
        <BatchDialog
          onClose={() => {
            setOpen(null);
          }}
          onSubmit={(b) => {
            setOpen(null);
            void job.start(b);
          }}
        />
      )}
    </div>
  );
}
