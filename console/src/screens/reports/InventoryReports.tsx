// Màn /bao-cao/ton-kho (Task 241, M6-18): hai tab, Tồn (mặc định) và Xuất nhập tồn (?tab=xuat-nhap-ton).
import { useSearchParams } from "react-router";
import { MovementsReport } from "./MovementsReport";
import { StockReport } from "./StockReport";
import { TAB_MOVEMENTS } from "./InventoryFilters";

export function InventoryReports() {
  const [sp] = useSearchParams();
  return sp.get("tab") === TAB_MOVEMENTS ? <MovementsReport /> : <StockReport />;
}
