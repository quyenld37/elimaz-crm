// Tab Xuất nhập tồn của /bao-cao/ton-kho (Task 241, M6-18; mockup docs/m6/mockup/ton-kho.html trạng thái C; BC03).
// Kỳ (cùng bộ chọn kỳ của các báo cáo khác), kho, đầu mã SKU; bảng đầu kỳ, nhập, nhập trả, xuất bán, xuất khác, kiểm kho /
// điều chỉnh (có dấu), cuối kỳ. Mọi số do máy chủ tính; màn không tính lại cuối kỳ.
import { useSearchParams } from "react-router";
import { getInventoryMovements, type InventoryMovementRow, type InventoryMovements } from "../../api";
import { unwrap } from "../../app/errors";
import { ExportButton, ExportNotice } from "./ExportButton";
import { InventoryFilters, InventoryTabs, TAB_MOVEMENTS, WarehouseScope, useSkuFilter } from "./InventoryFilters";
import { PeriodPicker } from "./PeriodPicker";
import { ReportsPage } from "./ReportsLayout";
import { PagedReport } from "./PagedReport";
import { ReloadButton, ReportBanners } from "./ReportBanners";
import type { VColumn } from "./VirtualTable";
import { count, dayLabel } from "./format";
import { STOCK_PAGE, inventoryRunApi, inventoryRunFile, parseMovements, useReportWarehouses } from "./inventoryData";
import { parsePeriod, periodError, vnToday, writePeriod, type Period } from "./period";
import { useMorePages } from "./useMorePages";
import { useReport } from "./useReport";
import { useExportRun } from "./useRunNotice";

const SUB = "Nhập, xuất và kiểm kho của từng mã hàng trong một kỳ, từ tồn đầu kỳ tới tồn cuối kỳ.";

/** Số có dấu cho kiểm kho / điều chỉnh: −1, +2, 0. */
export function signed(n: number): string {
  if (n < 0) return `−${count(-n)}`;
  return n > 0 ? `+${count(n)}` : "0";
}

const short = (iso: string) => dayLabel(iso).slice(0, 5);

export function MovementsReport() {
  const [sp, setSp] = useSearchParams();
  const today = vnToday(new Date());
  const period = parsePeriod(sp, today);
  const bad = periodError(period, today);
  const filter = useSkuFilter();
  const warehouses = useReportWarehouses();
  const names = new Map((warehouses.data ?? []).map((w) => [w.id, w.name]));

  const query = {
    from: period.from,
    to: period.to,
    ...(filter.warehouseId === "" ? {} : { warehouseId: filter.warehouseId }),
    ...(filter.q === "" ? {} : { q: filter.q }),
  };
  const rep = useReport<InventoryMovements>({
    report: "stock_movements",
    params: query,
    load: () => unwrap(getInventoryMovements({ query: { ...query, limit: STOCK_PAGE } })),
    parse: parseMovements,
    runApi: inventoryRunApi,
    enabled: bad === null,
  });
  const more = useMorePages<InventoryMovementRow>(rep.data, async (cursor) => {
    const p = await unwrap(getInventoryMovements({ query: { ...query, cursor, limit: STOCK_PAGE } }));
    if (p.status !== "ready") throw new Error("run_in_background");
    return p;
  });
  const exp = useExportRun({
    report: "stock_movements",
    params: query,
    title: `Xuất nhập tồn ${dayLabel(period.from)}${period.from === period.to ? "" : ` – ${dayLabel(period.to)}`}`,
    runApi: inventoryRunApi,
    runFile: inventoryRunFile,
  });
  const loading = rep.phase === "loading" && bad === null;

  const onPeriod = (p: Period) => {
    const next = new URLSearchParams(sp);
    writePeriod(next, p);
    setSp(next);
  };

  const columns: readonly VColumn<InventoryMovementRow>[] = [
    {
      header: "SKU",
      cell: (r) => <span title={r.productName}>{r.sku}</span>,
    },
    { header: "Kho", left: true, cell: (r) => names.get(r.warehouseId) ?? "—" },
    { header: "Đầu kỳ", cell: (r) => count(r.opening) },
    { header: "Nhập", cell: (r) => count(r.imported) },
    { header: "Nhập trả", cell: (r) => count(r.returnedIn) },
    { header: "Xuất bán", cell: (r) => count(r.sold) },
    { header: "Xuất khác", cell: (r) => count(r.exportedOther) },
    { header: "Kiểm kho / điều chỉnh", cell: (r) => signed(r.adjusted) },
    { header: "Cuối kỳ", bold: true, cell: (r) => count(r.closing) },
  ];

  return (
    <ReportsPage
      title="Tồn và xuất nhập tồn"
      sub={SUB}
      actions={
        <>
          <ReloadButton rep={rep} disabled={bad !== null} />
          <ExportButton exp={exp} disabled={loading || bad !== null} />
        </>
      }
    >
      <ExportNotice exp={exp} />
      <WarehouseScope />
      <InventoryTabs tab={TAB_MOVEMENTS} />
      <PeriodPicker
        key={`${period.preset}|${period.from}|${period.to}`}
        value={period}
        today={today}
        onChange={onPeriod}
        disabled={loading}
      />
      <InventoryFilters filter={filter} />

      {bad === null && <ReportBanners rep={rep} failedHint="Thử kỳ ngắn hơn (ví dụ một tháng), rồi bấm Tải lại." />}
      {rep.phase === "ready" && rep.data !== null && (
        <PagedReport
          label="Xuất nhập tồn"
          meta={`${short(rep.data.period.from)} – ${dayLabel(rep.data.period.to)} · đầu kỳ = tồn sổ lúc 00:00 ngày ${short(rep.data.period.from)}, cuối kỳ = lúc 24:00 ngày ${short(rep.data.period.to)} (giờ Việt Nam)`}
          more={more}
          columns={columns}
          rowKey={(r) => `${r.variantId}|${r.warehouseId}`}
          empty={
            <div className="box box-info stack-sm">
              <b>Không có nhập, xuất trong kỳ.</b>
              <p className="small">Tồn đầu kỳ bằng cuối kỳ. Chọn kỳ khác.</p>
            </div>
          }
          footnote="Cuối kỳ = Đầu kỳ + Nhập + Nhập trả − Xuất bán − Xuất khác ± Kiểm kho. Xuất khác: phiếu xuất hủy, xuất trả nhà cung cấp, chuyển kho đi."
        />
      )}
    </ReportsPage>
  );
}
