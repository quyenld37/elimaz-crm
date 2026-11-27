// Nút "Xuất danh sách sản phẩm" (Task 241, M6-18; hành vi (d), chủ shop chốt thay 27/11/2026; mockup nut-xuat.html trạng
// thái C). Dùng ở đầu màn Sản phẩm (M1) và ở tab Tồn. Chỉ hiện khi có report.inventory.read và report.export phạm vi
// toàn shop (máy chủ vẫn kiểm). Hộp chọn trạng thái mẫu và đầu mã SKU → POST …/inventory/runs {report: "products",
// format: "xlsx"} qua useWriteAction (một khóa cho một lần bấm) → "Đang tạo tệp, xem ở Tệp xuất".
import { useState } from "react";
import { useWriteAction } from "../../app/idempotency";
import { ExportDialog, ExportLauncher } from "./ExportDialog";
import { SkuInput } from "./InventoryFilters";
import { inventoryRunApi } from "./inventoryData";
import { useReportAccess } from "./reportAccess";

const STATUSES = [
  { value: "", label: "Mọi trạng thái" },
  { value: "draft", label: "Nháp" },
  { value: "active", label: "Đang bán" },
  { value: "discontinued", label: "Ngừng bán" },
] as const;

function ProductsExportDialog({ onClose }: { onClose: () => void }) {
  const [status, setStatus] = useState("");
  const [sku, setSku] = useState("");
  const [done, setDone] = useState(false);
  const write = useWriteAction((key: string, params: Record<string, unknown>) =>
    inventoryRunApi.create(key, { report: "products", format: "xlsx", params }),
  );
  const submit = () => {
    const q = sku.trim();
    const params = { ...(status === "" ? {} : { status }), ...(q === "" ? {} : { q }) };
    void write.run(params).then((r) => {
      if (r.ok) setDone(true);
    });
  };
  return (
    <ExportDialog
      title="Xuất danh sách sản phẩm"
      disabled={write.pending}
      error={write.message}
      done={done}
      onSubmit={submit}
      onClose={onClose}
    >
      <label className="field">
        <span>Trạng thái mẫu</span>
        <span className="select-wrap">
          <select
            value={status}
            onChange={(e) => {
              setStatus(e.target.value);
            }}
          >
            {STATUSES.map((s) => (
              <option key={s.value} value={s.value}>
                {s.label}
              </option>
            ))}
          </select>
        </span>
      </label>
      <SkuInput value={sku} onChange={setSku} />
      <p className="small muted">
        Cột như tệp mẫu Nhập Excel, không có giá vốn, cột cuối "Có thể bán" (cộng các kho bạn được xem). Tải được trong
        7 ngày, xem ở Tệp xuất.
      </p>
    </ExportDialog>
  );
}

export function ProductsExportButton() {
  const access = useReportAccess();
  if (!access.canExport || !access.inventoryAll) return null;
  return (
    <ExportLauncher label="Xuất danh sách sản phẩm" dialog={(close) => <ProductsExportDialog onClose={close} />} />
  );
}
