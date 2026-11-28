// Danh sách Đợt flash sale /khuyen-mai/flash-sale (kế hoạch M5 Task 206, M5-23; mockup docs/mockups/m5/flash-sales.html
// trạng thái A, B; HV26). Máy chủ trả mới nhất trước, lọc một trạng thái (?status=); tải thêm theo con trỏ; tự tải lại 20 s
// và khi nhận báo hiệu flash_sale.changed. Tạo đợt nháp là hộp thoại; sau đó mở trang của đợt.
import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useNavigate } from "react-router";
import { listFlashSales, type FlashSaleStatus, type FlashSaleSummary, type FlashSalePage } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { useNow } from "../live/LivePage";
import { ActCell, ActLink } from "../../shared/ActCell";
import { MoreBar } from "../ma-giam-gia/MoreBar";
import { FLASH_TOPIC, PERM_MANAGE, usePerms } from "./flashData";
import { SaleFormDialog } from "./FlashDialogs";
import { RetryBox } from "./RetryBox";
import { IDLE_MS, STATUS_FILTERS, filterText, periodText, saleSub, soldText, statusChip } from "./flashRules";
import "../don/don.css";
import "./flash-sales.css";

const PAGE = 20;

function Row({ s, now }: { s: FlashSaleSummary; now: number }) {
  const chip = statusChip(s.status);
  const go = s.status === "draft" ? "Sửa" : s.status === "closed" ? "Xem" : "Mở";
  return (
    <tr className={s.status === "closed" ? "is-muted" : undefined}>
      <td className="lead">
        <div className="nm">
          <b className="clamp2" title={s.name}>
            {s.name}
          </b>
          <span className="small muted">{saleSub(s, now)}</span>
        </div>
      </td>
      <td data-label="Giờ mở → giờ đóng (giờ VN)">
        <span className="num small">{periodText(s)}</span>
      </td>
      <td data-label="Trạng thái">
        <span className={chip.className}>{chip.label}</span>
      </td>
      <td data-label="Đã bán / suất" className="r num">
        {soldText(s)}
      </td>
      <ActCell>
        <ActLink to={`/khuyen-mai/flash-sale/${s.id}`}>{go}</ActLink>
      </ActCell>
    </tr>
  );
}

export function FlashSaleList() {
  const perms = usePerms();
  const navigate = useNavigate();
  const [status, setStatus] = useState<FlashSaleStatus | "">("");
  const [creating, setCreating] = useState(false);
  const canManage = perms.can(PERM_MANAGE);

  const query = { ...(status === "" ? {} : { status }), limit: PAGE };
  const list = useInfiniteQuery({
    queryKey: queryKey(FLASH_TOPIC, "list", query),
    initialPageParam: "",
    refetchInterval: IDLE_MS,
    getNextPageParam: (last: FlashSalePage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(listFlashSales({ query: { ...query, ...(pageParam === "" ? {} : { cursor: pageParam }) } })),
  });
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  const now = useNow(30_000);

  const create = canManage && (
    <button
      type="button"
      className="btn btn-dark"
      onClick={() => {
        setCreating(true);
      }}
    >
      + Tạo đợt
    </button>
  );

  function body() {
    if (list.isError) {
      return (
        <RetryBox
          title="Chưa tải được danh sách đợt."
          text="Kiểm tra mạng rồi thử lại. Danh sách cũ không hiện để tránh thao tác nhầm trên đợt đã đổi."
          onRetry={() => {
            void list.refetch();
          }}
        />
      );
    }
    if (list.isSuccess && rows.length === 0) {
      return status === "" ? (
        <div className="empty">
          <h2>Chưa có đợt flash sale</h2>
          <p className="muted">Tạo đợt nháp, thêm món và số suất, rồi bấm Lên lịch để giữ suất.</p>
          {create}
        </div>
      ) : (
        <div className="empty">
          <h2>Không có đợt nào ở trạng thái này</h2>
        </div>
      );
    }
    if (rows.length === 0) return null;
    return (
      <>
        <div className="tbl-frame">
          <table className="tbl fsl">
            <thead>
              <tr>
                <th>Đợt</th>
                <th>Giờ mở → giờ đóng (giờ VN)</th>
                <th>Trạng thái</th>
                <th className="r">Đã bán / suất</th>
                <th className="c-act">Thao tác</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => (
                <Row key={s.id} s={s} now={now} />
              ))}
            </tbody>
          </table>
        </div>
        <MoreBar
          text={`Hiện ${String(rows.length)} đợt · mới nhất trước · tự cập nhật mỗi 20 giây`}
          pages={list}
          label="Xem thêm"
        />
      </>
    );
  }

  return (
    <div className="fs">
      <div className="page-h">
        <div className="grow">
          <p className="crumb">Khuyến mãi / Flash sale</p>
          <h1>Flash sale</h1>
          <p className="sub">
            Đợt bán giá rất thấp, số suất có hạn, mở đúng giờ trên website. Giờ nhập theo giờ Việt Nam. Bấm{" "}
            <b>Lên lịch</b> là số suất được giữ riêng khỏi bán thường ngay (lên đơn, chat, website), không đợi tới giờ
            mở.
          </p>
        </div>
        <div className="row">{create}</div>
      </div>
      {perms.loaded && !canManage && (
        <p className="reason">Bạn chỉ có quyền xem flash sale (thiếu quyền "Quản lý flash sale").</p>
      )}
      <div className="scroll-x">
        <div className="lblbar" role="group" aria-label="Lọc theo trạng thái">
          {STATUS_FILTERS.map((s) => (
            <button
              key={s}
              type="button"
              className={s === "open" ? "lbl is-attn" : "lbl"}
              aria-pressed={status === s}
              onClick={() => {
                setStatus(s);
              }}
            >
              {filterText(s)}
            </button>
          ))}
        </div>
      </div>
      {body()}
      {creating && (
        <SaleFormDialog
          onClose={() => {
            setCreating(false);
          }}
          onDone={(s) => {
            setCreating(false);
            void navigate(`/khuyen-mai/flash-sale/${s.sale.id}`);
          }}
        />
      )}
    </div>
  );
}
