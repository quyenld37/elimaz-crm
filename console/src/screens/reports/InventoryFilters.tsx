// Thanh tab và bộ lọc dùng chung của màn Tồn và xuất nhập tồn (Task 241, M6-18; mockup ton-kho.html: .tabs, .rep-f).
import { useState } from "react";
import { useSearchParams } from "react-router";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import { useReportWarehouses } from "./inventoryData";
import { useReportAccess } from "./reportAccess";

export const TAB_STOCK = "ton";
export const TAB_MOVEMENTS = "xuat-nhap-ton";
export const MAX_SKU = 64;

export function InventoryTabs({ tab }: { tab: typeof TAB_STOCK | typeof TAB_MOVEMENTS }) {
  const [, setSp] = useSearchParams();
  const go = (t: string) => {
    if (t === tab) return;
    setSp(t === TAB_STOCK ? {} : { tab: t });
  };
  return (
    <div className="tabs" role="tablist">
      <button
        type="button"
        className="tab"
        role="tab"
        aria-selected={tab === TAB_STOCK}
        onClick={() => {
          go(TAB_STOCK);
        }}
      >
        Tồn
      </button>
      <button
        type="button"
        className="tab"
        role="tab"
        aria-selected={tab === TAB_MOVEMENTS}
        onClick={() => {
          go(TAB_MOVEMENTS);
        }}
      >
        Xuất nhập tồn
      </button>
    </div>
  );
}

export interface SkuFilter {
  /** Kho đang chọn (rỗng: mọi kho). */
  warehouseId: string;
  setWarehouseId: (id: string) => void;
  /** Chữ đang gõ trong ô mã SKU. */
  text: string;
  setText: (t: string) => void;
  /** Đầu mã SKU đã bỏ dấu cách, sau 300 ms ngừng gõ; rỗng thì không gửi. */
  q: string;
}

/** Kho (nằm trên địa chỉ, ?kho=) và đầu mã SKU (gõ xong 300 ms mới gửi). */
export function useSkuFilter(): SkuFilter {
  const [sp, setSp] = useSearchParams();
  const [text, setText] = useState("");
  const q = useDebounced(text.trim().slice(0, MAX_SKU), SEARCH_DELAY_MS);
  const warehouseId = sp.get("kho") ?? "";
  const setWarehouseId = (id: string) => {
    setSp(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (id === "") next.delete("kho");
        else next.set("kho", id);
        return next;
      },
      { replace: true },
    );
  };
  return { warehouseId, setWarehouseId, text, setText, q };
}

/** Ô "Mã SKU bắt đầu bằng" (bộ lọc của báo cáo tồn và hộp xuất danh sách sản phẩm). */
export function SkuInput({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <label className="field">
      <span>Mã SKU bắt đầu bằng</span>
      <input
        className="input"
        placeholder="VD: AO-A"
        maxLength={MAX_SKU}
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
        }}
      />
    </label>
  );
}

export function InventoryFilters({ filter, children }: { filter: SkuFilter; children?: React.ReactNode }) {
  const warehouses = useReportWarehouses();
  return (
    <div className="rep-f">
      {children}
      <label className="field">
        <span>Kho</span>
        <span className="select-wrap">
          <select
            value={filter.warehouseId}
            onChange={(e) => {
              filter.setWarehouseId(e.target.value);
            }}
          >
            <option value="">Mọi kho</option>
            {(warehouses.data ?? []).map((w) => (
              <option key={w.id} value={w.id}>
                {w.name}
              </option>
            ))}
          </select>
        </span>
      </label>
      <SkuInput value={filter.text} onChange={filter.setText} />
    </div>
  );
}

/** Dòng "Phạm vi" cho người chỉ có quyền đọc tồn của một số kho (mockup trạng thái D). */
export function WarehouseScope() {
  const access = useReportAccess();
  const warehouses = useReportWarehouses();
  if (!access.known || access.inventoryAll || access.inventoryWarehouseIds.length === 0) return null;
  const names = access.inventoryWarehouseIds.map(
    (id) => warehouses.data?.find((w) => w.id === id)?.name ?? "kho được giao",
  );
  return (
    <div className="box box-info small">
      {`Phạm vi: ${names.join(", ")}. Ô chọn kho chỉ có kho của bạn. Xuất tệp cần quyền xuất ở phạm vi toàn shop.`}
    </div>
  );
}
