// Nút "Xuất danh sách khách" (Task 241, M6-18; hành vi (c), chủ shop chốt thay 27/11/2026; mockup m6/nut-xuat.html trạng
// thái A–B). Chỉ hiện khi có customer.export phạm vi toàn shop (Chủ, Quản lý; máy chủ vẫn kiểm). Hộp chọn bộ lọc theo
// CustomerExportParams (nhãn, loại khách, ngày tạo, ngày mua cuối, số đơn tối thiểu); KHÔNG có ô gõ chữ tự do, để tham số
// (và nhật ký) không chứa dữ liệu khách. POST /api/v1/reports/customers/exports qua useWriteAction (một khóa cho một lần
// bấm) → "Đang tạo tệp, xem ở Tệp xuất".
import { useState } from "react";
import { createCustomerExport } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { ExportDialog, ExportLauncher } from "../reports/ExportDialog";
import { useReportAccess } from "../reports/reportAccess";
import { useTags } from "./customerData";

export const TEXT_BAD_RANGE = "Ngày bắt đầu sau ngày kết thúc.";
const MAX_MIN_ORDERS = 100_000;

interface Form {
  tagIds: string[];
  lifecycle: string;
  createdFrom: string;
  createdTo: string;
  lastOrderFrom: string;
  lastOrderTo: string;
  minOrders: string;
}

const EMPTY: Form = {
  tagIds: [],
  lifecycle: "",
  createdFrom: "",
  createdTo: "",
  lastOrderFrom: "",
  lastOrderTo: "",
  minOrders: "",
};

const badRange = (a: string, b: string) => a !== "" && b !== "" && a > b;

/** Tham số gửi lên: chỉ trường có giá trị (không có thì để máy chủ hiểu là không lọc). */
export function exportParams(f: Form): Record<string, unknown> {
  const n = Number(f.minOrders);
  return {
    ...(f.createdFrom === "" ? {} : { createdFrom: f.createdFrom }),
    ...(f.createdTo === "" ? {} : { createdTo: f.createdTo }),
    ...(f.lastOrderFrom === "" ? {} : { lastOrderFrom: f.lastOrderFrom }),
    ...(f.lastOrderTo === "" ? {} : { lastOrderTo: f.lastOrderTo }),
    ...(f.lifecycle === "" ? {} : { lifecycle: f.lifecycle }),
    ...(f.tagIds.length === 0 ? {} : { tagIds: f.tagIds }),
    ...(f.minOrders !== "" && Number.isInteger(n) && n >= 1 ? { minOrders: n } : {}),
  };
}

function ExportCustomersDialog({ onClose }: { onClose: () => void }) {
  const tags = useTags();
  const [form, setForm] = useState<Form>(EMPTY);
  const [done, setDone] = useState(false);
  const write = useWriteAction((key: string, params: Record<string, unknown>) =>
    unwrap(createCustomerExport({ body: { params }, headers: { "Idempotency-Key": key } })),
  );
  const set = (patch: Partial<Form>) => {
    setForm({ ...form, ...patch });
  };
  const rangeBad = badRange(form.createdFrom, form.createdTo) || badRange(form.lastOrderFrom, form.lastOrderTo);
  const n = Number(form.minOrders);
  const ordersBad = form.minOrders !== "" && !(Number.isInteger(n) && n >= 1 && n <= MAX_MIN_ORDERS);
  const toggleTag = (id: string) => {
    set({ tagIds: form.tagIds.includes(id) ? form.tagIds.filter((t) => t !== id) : [...form.tagIds, id] });
  };
  const submit = () => {
    void write.run(exportParams(form)).then((r) => {
      if (r.ok) setDone(true);
    });
  };
  const date = (label: string, key: "createdFrom" | "createdTo" | "lastOrderFrom" | "lastOrderTo") => (
    <label className="field">
      <span>{label}</span>
      <input
        className="input num"
        type="date"
        value={form[key]}
        onChange={(e) => {
          set({ [key]: e.target.value });
        }}
      />
    </label>
  );
  return (
    <ExportDialog
      title="Xuất danh sách khách"
      disabled={write.pending || rangeBad || ordersBad}
      error={write.message}
      done={done}
      onSubmit={submit}
      onClose={onClose}
    >
      <p className="small">
        Tệp có tên và SĐT khách. Hết hạn tải sau 24 giờ; chỉ bạn và người có quyền quản lý tệp xuất tải được. Lần xuất,
        lúc xong và mỗi lần tải đều ghi vào Nhật ký thao tác.
      </p>
      {(tags.data?.items.length ?? 0) > 0 && (
        <fieldset className="field">
          <legend>Nhãn (chọn nhiều)</legend>
          <div className="row">
            {tags.data?.items.map((t) => (
              <label key={t.id} className="row">
                <input
                  type="checkbox"
                  checked={form.tagIds.includes(t.id)}
                  onChange={() => {
                    toggleTag(t.id);
                  }}
                />
                <span>{t.name}</span>
              </label>
            ))}
          </div>
        </fieldset>
      )}
      <label className="field">
        <span>Loại khách</span>
        <span className="select-wrap">
          <select
            value={form.lifecycle}
            onChange={(e) => {
              set({ lifecycle: e.target.value });
            }}
          >
            <option value="">Mọi loại</option>
            <option value="customer">Đã mua</option>
            <option value="lead">Tiềm năng</option>
          </select>
        </span>
      </label>
      <div className="rep-f">
        {date("Tạo từ ngày", "createdFrom")}
        {date("Tạo đến ngày", "createdTo")}
        {date("Mua lần cuối từ ngày", "lastOrderFrom")}
        {date("Mua lần cuối đến ngày", "lastOrderTo")}
      </div>
      {rangeBad && <p className="field-err">{TEXT_BAD_RANGE}</p>}
      <label className="field">
        <span>Số đơn tối thiểu</span>
        <input
          className="input num"
          type="number"
          min={1}
          max={MAX_MIN_ORDERS}
          value={form.minOrders}
          onChange={(e) => {
            set({ minOrders: e.target.value });
          }}
        />
      </label>
      {ordersBad && <p className="field-err">Số đơn tối thiểu là số nguyên từ 1.</p>}
      <p className="small muted">
        Bộ lọc chỉ gồm ô chọn, ngày và số, không có ô gõ chữ (để nhật ký không chứa dữ liệu khách). Cột trong tệp: mã
        khách, tên, SĐT chính, số đơn, thành công, hoàn, tỷ lệ hoàn, tổng chi, ngày mua cuối, nhãn, ngày tạo. Không có
        địa chỉ.
      </p>
    </ExportDialog>
  );
}

export function ExportCustomersButton() {
  const access = useReportAccess();
  if (!access.customersExport) return null;
  return <ExportLauncher label="Xuất danh sách khách" dialog={(close) => <ExportCustomersDialog onClose={close} />} />;
}
