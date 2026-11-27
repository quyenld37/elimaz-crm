// Tab Tồn của /bao-cao/ton-kho (Task 241, M6-18; mockup docs/m6/mockup/ton-kho.html trạng thái A, B, D; BC03).
// Hôm nay (số dư hiện tại) hay một ngày đã qua (bản chụp 24:00); chọn kho; tìm theo đầu mã SKU; bảng cuộn ảo, tải 500
// dòng mỗi lần. Ngày trước bản chụp đầu → 404 no_snapshot → "Chưa có bản chụp trước ngày dd/mm/yyyy" (ngày lấy từ
// firstSnapshotDay của một lần gọi hôm nay). "Có thể bán" là số máy chủ trả (tồn sổ − giữ − đang giao − lỗi), không tính lại.
import { useState } from "react";
import { useSearchParams } from "react-router";
import {
  getInventoryStock,
  type InventoryStock,
  type InventoryStockRow,
  type ReportParams,
  type RunInBackground,
} from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { ExportButton, ExportNotice } from "./ExportButton";
import { InventoryFilters, InventoryTabs, TAB_STOCK, WarehouseScope, useSkuFilter } from "./InventoryFilters";
import { ProductsExportButton } from "./ProductsExportButton";
import { ReportsPage } from "./ReportsLayout";
import { PagedReport } from "./PagedReport";
import { ReloadButton, ReportBanners } from "./ReportBanners";
import type { VColumn } from "./VirtualTable";
import { clockTime, count, dayLabel } from "./format";
import { STOCK_PAGE, inventoryRunApi, inventoryRunFile, parseStock } from "./inventoryData";
import { isDay, vnToday } from "./period";
import { useMorePages } from "./useMorePages";
import { useReport } from "./useReport";
import { useExportRun } from "./useRunNotice";

export const TEXT_FUTURE_DAY = "Không chọn ngày tương lai.";
const SUB = "Tồn hôm nay hoặc tồn cuối một ngày đã qua; nhập, xuất tồn trong một kỳ ở tab Xuất nhập tồn.";

/** Kết quả hiện được: bảng tồn, hay "chưa có bản chụp" (404 no_snapshot) kèm ngày bản chụp đầu. */
type StockView =
  | { status: "ready"; kind: "stock"; data: InventoryStock }
  | { status: "ready"; kind: "no_snapshot"; date: string; firstSnapshotDay: string | undefined };

interface StockQuery extends ReportParams {
  date?: string;
  warehouseId?: string;
  q?: string;
}

const wrap = (r: InventoryStock): StockView => ({ status: "ready", kind: "stock", data: r });

async function loadStock(query: StockQuery): Promise<StockView | RunInBackground> {
  try {
    const r = await unwrap(getInventoryStock({ query: { ...query, limit: STOCK_PAGE } }));
    return r.status === "run_in_background" ? r : wrap(r);
  } catch (e) {
    if (e instanceof ApiError && e.code === "no_snapshot" && query.date !== undefined) {
      // Ngày bản chụp đầu nằm ở lần gọi hôm nay (một dòng là đủ).
      const today = await unwrap(getInventoryStock({ query: { limit: 1 } }));
      if (today.status === "ready") {
        return { status: "ready", kind: "no_snapshot", date: query.date, firstSnapshotDay: today.firstSnapshotDay };
      }
    }
    throw e;
  }
}

const COLUMNS: readonly VColumn<InventoryStockRow>[] = [
  { header: "SKU", cell: (r) => r.sku },
  {
    header: "Tên",
    left: true,
    cell: (r) => (
      <span className="nm is-wide" title={r.productName}>
        {r.productName}
      </span>
    ),
  },
  { header: "Màu / size", left: true, cell: (r) => `${r.color} / ${r.size}` },
  { header: "Kho", left: true, cell: (r) => r.warehouseName },
  { header: "Tồn sổ", cell: (r) => count(r.onHand) },
  { header: "Đang giữ", cell: (r) => count(r.held) },
  { header: "Đang giao", cell: (r) => count(r.inTransit) },
  { header: "Hàng lỗi", cell: (r) => count(r.damaged) },
  {
    header: "Có thể bán",
    bold: true,
    cell: (r) => (r.available <= 0 ? <span className="err-text">{count(r.available)}</span> : count(r.available)),
  },
];

function NoSnapshot({ view }: { view: Extract<StockView, { kind: "no_snapshot" }> }) {
  const first = view.firstSnapshotDay;
  if (first !== undefined && view.date < first) {
    return (
      <div className="box box-info stack-sm">
        <b>{`Chưa có bản chụp trước ngày ${dayLabel(first)}.`}</b>
        <p className="small">
          {`Bạn chọn ${dayLabel(view.date)}. Hệ thống bắt đầu chụp tồn từ ${dayLabel(first)}; chọn từ ngày đó trở đi.`}
        </p>
      </div>
    );
  }
  return (
    <div className="box box-info stack-sm">
      <b>{`Chưa có số tồn của ngày ${dayLabel(view.date)}.`}</b>
      <p className="small">Chọn ngày khác.</p>
    </div>
  );
}

export function StockReport() {
  const [sp, setSp] = useSearchParams();
  const today = vnToday(new Date());
  const dateParam = sp.get("ngay") ?? "";
  const date = isDay(dateParam) && dateParam < today ? dateParam : "";
  const [choosing, setChoosing] = useState(date !== "");
  const [typed, setTyped] = useState(date);
  const filter = useSkuFilter();

  const setDate = (d: string) => {
    setSp(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (d === "") next.delete("ngay");
        else next.set("ngay", d);
        return next;
      },
      { replace: true },
    );
  };
  const futureDay = typed !== "" && isDay(typed) && typed > today;
  const onDate = (v: string) => {
    setTyped(v);
    if (v === "") setDate("");
    else if (isDay(v) && v < today) setDate(v);
    else if (isDay(v) && v === today) setDate("");
  };

  const query: StockQuery = {
    ...(date === "" ? {} : { date }),
    ...(filter.warehouseId === "" ? {} : { warehouseId: filter.warehouseId }),
    ...(filter.q === "" ? {} : { q: filter.q }),
  };
  const rep = useReport<StockView>({
    report: "stock",
    params: query,
    load: () => loadStock(query),
    parse: (v) => {
      const s = parseStock(v);
      return s === null ? null : wrap(s);
    },
    runApi: inventoryRunApi,
  });
  const view = rep.data;
  const first = view?.kind === "stock" ? view.data : null;
  const more = useMorePages<InventoryStockRow>(first, async (cursor) => {
    const p = await unwrap(getInventoryStock({ query: { ...query, cursor, limit: STOCK_PAGE } }));
    if (p.status !== "ready") throw new Error("run_in_background");
    return p;
  });
  const exp = useExportRun({
    report: "stock",
    params: query,
    title: date === "" ? `Tồn ${dayLabel(today)}` : `Tồn ${dayLabel(date)}`,
    runApi: inventoryRunApi,
    runFile: inventoryRunFile,
  });

  return (
    <ReportsPage
      title="Tồn và xuất nhập tồn"
      sub={SUB}
      actions={
        <>
          <ReloadButton rep={rep} />
          <ProductsExportButton />
          <ExportButton exp={exp} disabled={rep.phase === "loading"} />
        </>
      }
    >
      <ExportNotice exp={exp} />
      <WarehouseScope />
      <InventoryTabs tab={TAB_STOCK} />
      <InventoryFilters filter={filter}>
        <div className="field">
          <span>Ngày</span>
          <div className="row" role="group" aria-label="Ngày">
            <button
              type="button"
              className="toggle"
              aria-pressed={!choosing}
              onClick={() => {
                setChoosing(false);
                setTyped("");
                setDate("");
              }}
            >
              Hôm nay
            </button>
            <button
              type="button"
              className="toggle"
              aria-pressed={choosing}
              onClick={() => {
                setChoosing(true);
              }}
            >
              Chọn ngày
            </button>
          </div>
        </div>
        {choosing && (
          <label className="field">
            <span>Ngày đã qua</span>
            <input
              className="input num"
              type="date"
              max={today}
              value={typed}
              onChange={(e) => {
                onDate(e.target.value);
              }}
            />
          </label>
        )}
      </InventoryFilters>
      {futureDay && <p className="field-err">{TEXT_FUTURE_DAY}</p>}

      <ReportBanners rep={rep} failedHint="Chọn kho hay đầu mã SKU hẹp hơn, rồi bấm Tải lại." />
      {rep.phase === "ready" && view?.kind === "no_snapshot" && <NoSnapshot view={view} />}
      {rep.phase === "ready" && first !== null && rep.updatedAt !== null && (
        <PagedReport
          label="Tồn"
          meta={
            first.source === "live"
              ? `Tồn lúc ${clockTime(rep.updatedAt)} · ${dayLabel(first.date)} (số hiện tại)`
              : `Tồn cuối ngày ${dayLabel(first.date)} · bản chụp lúc 24:00 giờ Việt Nam (không tính lại từ đầu)`
          }
          more={more}
          columns={COLUMNS}
          rowKey={(r) => `${r.variantId}|${r.warehouseId}`}
          empty={<div className="box box-info">Không có mã hàng nào còn tồn theo bộ lọc này.</div>}
          footnote="Có thể bán = Tồn sổ − Đang giữ − Đang giao − Hàng lỗi (cùng công thức màn Tồn kho của Kho)."
        />
      )}
    </ReportsPage>
  );
}
