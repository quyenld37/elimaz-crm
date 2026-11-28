// Trang một đợt flash sale /khuyen-mai/flash-sale/:id (kế hoạch M5 Task 206, M5-23; mockup
// docs/mockups/m5/flash-sales.html trạng thái B–H; HV27–HV31). Nháp: sửa tên, giờ; thêm, sửa, bỏ món; Lên lịch. Đã lên
// lịch hay đang mở: chỉ Thêm suất, Đóng sớm (số trực tiếp). Đã đóng: chỉ xem. Nút ghi ẩn khi thiếu promotion.flash.manage
// (máy chủ vẫn chặn). Mọi hộp thoại ở FlashDialogs.tsx.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useParams } from "react-router";
import type { FlashItem } from "../../api";
import { ApiError, errorMessage } from "../../app/errors";
import { useNow } from "../live/LivePage";
import { formatMoney } from "../products/productRules";
import { PERM_MANAGE, reloadFlash, useFlashLive, useFlashSale, usePerms } from "./flashData";
import {
  AddSlotsDialog,
  CloseDialog,
  ItemFormDialog,
  RemoveItemDialog,
  SaleFormDialog,
  ScheduleDialog,
} from "./FlashDialogs";
import { FlashSaleLive } from "./FlashSaleLive";
import { RetryBox } from "./RetryBox";
import { actionsOf, longTime, remainingText, statusChip } from "./flashRules";
import "../don/don.css";
import "./flash-sales.css";

type Dlg =
  { kind: "sale" | "add" | "schedule" | "close" } | { kind: "edit" | "remove" | "slots"; item: FlashItem } | null;

const num = (n: number) => new Intl.NumberFormat("vi-VN").format(n);
const clock = (ms: number) =>
  ms === 0
    ? ""
    : new Intl.DateTimeFormat("en-GB", { timeZone: "Asia/Ho_Chi_Minh", timeStyle: "medium", hourCycle: "h23" }).format(
        ms,
      );

function DraftTable(o: {
  items: readonly FlashItem[];
  canEdit: boolean;
  onEdit: (i: FlashItem) => void;
  onRemove: (i: FlashItem) => void;
}) {
  return (
    <div className="tbl-frame">
      <table className="tbl fsi">
        <thead>
          <tr>
            <th>Món</th>
            <th className="r">Giá thường</th>
            <th className="r">Giá flash</th>
            <th className="r">Suất</th>
            <th className="r">Mỗi khách</th>
            <th className="r">Hạn giữ</th>
            <th className="r">Có thể bán lúc này</th>
            <th className="c-act">Thao tác</th>
          </tr>
        </thead>
        <tbody>
          {o.items.map((i) => (
            <tr key={i.id}>
              <td className="lead">
                <div className="sp">
                  <span className="nm clamp2" title={i.name}>
                    {i.name}
                  </span>
                  <span className="code">{i.sku}</span>
                </div>
              </td>
              <td data-label="Giá thường" className="r">
                <span className="money">{formatMoney(i.listPrice)}</span>
              </td>
              <td data-label="Giá flash" className="r">
                <b className="money">{formatMoney(i.salePrice)}</b>
              </td>
              <td data-label="Suất" className="r num">
                {num(i.quantity)}
              </td>
              <td data-label="Mỗi khách" className="r num">
                {i.perCustomerLimit}
              </td>
              <td data-label="Hạn giữ" className="r num">
                {`${String(i.holdMinutes)} phút`}
              </td>
              <td data-label="Có thể bán lúc này" className={i.sellable < i.quantity ? "r num err-text" : "r num"}>
                {num(i.sellable)}
              </td>
              <td className="c-act">
                <div className="row">
                  {o.canEdit && (
                    <>
                      <button
                        type="button"
                        className="btn btn-quiet"
                        onClick={() => {
                          o.onEdit(i);
                        }}
                      >
                        Sửa
                      </button>
                      <button
                        type="button"
                        className="btn btn-quiet"
                        onClick={() => {
                          o.onRemove(i);
                        }}
                      >
                        Bỏ
                      </button>
                    </>
                  )}
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function FlashSaleEditor() {
  const { id = "" } = useParams();
  const perms = usePerms();
  const qc = useQueryClient();
  const q = useFlashSale(id);
  const sale = q.data?.sale;
  const items = q.data?.items ?? [];
  const live = useFlashLive(id, sale !== undefined && sale.status !== "draft", sale?.status);
  const now = useNow(30_000);
  const [dlg, setDlg] = useState<Dlg>(null);
  const canManage = perms.can(PERM_MANAGE);
  const close = () => {
    setDlg(null);
  };
  const done = () => {
    setDlg(null);
    reloadFlash(qc);
  };

  if (q.isError) {
    const gone = q.error instanceof ApiError && q.error.status === 404;
    return (
      <div className="fs">
        <p className="crumb">
          <Link to="/khuyen-mai/flash-sale">Khuyến mãi / Flash sale</Link>
        </p>
        {gone ? (
          <div className="empty">
            <h2>Không tìm thấy đợt flash sale.</h2>
            <Link className="btn btn-line" to="/khuyen-mai/flash-sale">
              Về danh sách
            </Link>
          </div>
        ) : (
          <RetryBox
            title="Chưa tải được đợt."
            text={errorMessage(q.error)}
            onRetry={() => {
              void q.refetch();
            }}
          />
        )}
      </div>
    );
  }
  if (sale === undefined) {
    return (
      <div className="fs">
        <p className="muted" role="status">
          Đang tải đợt…
        </p>
      </div>
    );
  }

  const chip = statusChip(sale.status);
  const act = actionsOf(sale.status, canManage);
  const total = items.reduce((n, i) => n + i.quantity, 0);
  const taken = new Set(items.map((i) => i.variantId));
  const sub = [
    `${longTime(sale.startsAt)} → ${longTime(sale.endsAt)} (giờ VN)`,
    `${String(items.length)} món`,
    `${num(total)} suất`,
    sale.status === "draft" ? "chưa giữ suất nào" : null,
    sale.status === "open" ? remainingText(sale.endsAt, now) : null,
    sale.status === "closed" ? (sale.closedReason === "manual" ? "đóng sớm bằng tay" : "đã hết giờ") : null,
  ]
    .filter((x): x is string => x !== null)
    .join(" · ");
  const liveItems = live.data?.items;
  const slotsItem = dlg?.kind === "slots" ? dlg.item : undefined;

  return (
    <div className="fs">
      <div className="page-h">
        <div className="grow">
          <p className="crumb">
            <Link to="/khuyen-mai/flash-sale">Khuyến mãi / Flash sale</Link>
          </p>
          <h1 className="wrap-any">
            {sale.name} <span className={chip.className}>{chip.label}</span>
          </h1>
          <p className="sub num">
            {sub}
            {sale.status === "open" && live.isSuccess
              ? ` · cập nhật ${clock(live.dataUpdatedAt)}, tự làm mới mỗi 5 giây`
              : ""}
          </p>
        </div>
        <div className="row">
          {sale.status === "open" && (
            <button
              type="button"
              className="btn btn-line"
              onClick={() => {
                reloadFlash(qc);
              }}
            >
              Tải lại
            </button>
          )}
          {act.edit && (
            <>
              <button
                type="button"
                className="btn btn-line"
                onClick={() => {
                  setDlg({ kind: "sale" });
                }}
              >
                Sửa tên, giờ
              </button>
              <button
                type="button"
                className="btn btn-line"
                onClick={() => {
                  setDlg({ kind: "add" });
                }}
              >
                + Thêm món
              </button>
            </>
          )}
          {act.schedule && (
            <button
              type="button"
              className="btn btn-dark"
              disabled={items.length === 0}
              onClick={() => {
                setDlg({ kind: "schedule" });
              }}
            >
              Lên lịch
            </button>
          )}
          {act.close && (
            <button
              type="button"
              className="btn btn-danger"
              onClick={() => {
                setDlg({ kind: "close" });
              }}
            >
              Đóng sớm
            </button>
          )}
        </div>
      </div>

      {perms.loaded && !canManage && (
        <p className="reason">Bạn chỉ có quyền xem flash sale (thiếu quyền "Quản lý flash sale").</p>
      )}
      {sale.status === "scheduled" && (
        <p className="reason">
          Đợt đã lên lịch nên không sửa được món, giá, giờ. Cần đổi thì Đóng sớm (suất trả về bán thường) rồi tạo đợt
          mới.
        </p>
      )}
      {sale.status === "open" && (
        <p className="reason">Đợt đang mở nên không sửa được món, giá, giờ; chỉ Thêm suất hoặc Đóng sớm.</p>
      )}
      {sale.status === "closed" && <p className="reason">Đợt đã đóng: chỉ xem. Muốn bán tiếp thì tạo đợt mới.</p>}

      {sale.status === "draft" ? (
        <>
          {items.length === 0 ? (
            <div className="empty">
              <h2>Đợt chưa có món</h2>
              <p className="muted">Thêm ít nhất một món rồi bấm Lên lịch.</p>
            </div>
          ) : (
            <DraftTable
              items={items}
              canEdit={act.edit}
              onEdit={(item) => {
                setDlg({ kind: "edit", item });
              }}
              onRemove={(item) => {
                setDlg({ kind: "remove", item });
              }}
            />
          )}
          <p className="small muted fs-gap">
            "Có thể bán lúc này" là tồn bán thường hiện tại của biến thể (chữ đỏ: ít hơn số suất, Lên lịch sẽ bị từ
            chối). Mỗi lượt mua flash là một đơn riêng chỉ có món đó, không vào giỏ chung, không dùng kèm mã giảm giá.
          </p>
        </>
      ) : (
        <FlashSaleLive
          status={sale.status}
          items={items}
          live={liveItems}
          error={live.error}
          canAddSlots={act.addSlots}
          onRetry={() => {
            void live.refetch();
          }}
          onAddSlots={(item) => {
            setDlg({ kind: "slots", item });
          }}
        />
      )}

      {dlg?.kind === "sale" && <SaleFormDialog sale={sale} onClose={close} onDone={done} />}
      {dlg?.kind === "add" && <ItemFormDialog sale={sale} taken={taken} onClose={close} onDone={done} />}
      {dlg?.kind === "edit" && (
        <ItemFormDialog sale={sale} item={dlg.item} taken={taken} onClose={close} onDone={done} />
      )}
      {dlg?.kind === "remove" && <RemoveItemDialog sale={sale} item={dlg.item} onClose={close} onDone={done} />}
      {dlg?.kind === "schedule" && <ScheduleDialog sale={sale} items={items} onClose={close} onDone={done} />}
      {dlg?.kind === "close" && <CloseDialog sale={sale} live={liveItems} onClose={close} onDone={done} />}
      {slotsItem !== undefined && (
        <AddSlotsDialog
          sale={sale}
          item={slotsItem}
          live={liveItems?.find((l) => l.itemId === slotsItem.id)}
          onClose={close}
          onDone={done}
        />
      )}
    </div>
  );
}
