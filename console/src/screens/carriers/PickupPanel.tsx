// Thẻ "Địa chỉ lấy hàng · <kho>" trên trang sửa tài khoản (mockup docs/mockups/m3/ket-noi-van-chuyen.html trạng thái C).
// Của kho, dùng cho mọi hãng gửi từ kho này; không phải địa chỉ khách. Đọc getWarehousePickup (địa chỉ đang lưu và
// version), ghi setWarehousePickup kèm version (409 → tải lại). Quyền fulfillment.settings.manage (máy chủ kiểm); tỉnh,
// xã theo danh mục listAdminUnits như màn Lên đơn.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import {
  getWarehousePickup,
  listAdminUnits,
  setWarehousePickup,
  type SetWarehousePickupRequest,
  type WarehousePickup,
} from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { WardField } from "../len-don/AddressFields";
import type { AddressInput } from "../len-don/orderRules";
import { RefusalBox } from "../staff/staffData";
import { QueryState } from "./QueryState";
import "../len-don/len-don.css";

const pickupKey = (warehouseId: string) => queryKey("inventory.warehouse_pickup", warehouseId);
const ADMIN_TOPIC = "customer.admin-units";

interface PickupFormProps {
  p: WarehousePickup;
  warehouseId: string;
  /** "Đã lưu" giữ ở thẻ cha: form dựng lại theo version mới sau khi lưu. */
  saved: boolean;
  setSaved: (v: boolean) => void;
}

function PickupForm({ p, warehouseId, saved, setSaved }: PickupFormProps) {
  const qc = useQueryClient();
  const [v, setV] = useState<AddressInput>({
    name: p.name ?? "",
    phone: p.phone ?? "",
    line: p.line ?? "",
    provinceCode: p.provinceCode ?? "",
    wardCode: p.wardCode ?? "",
  });
  const [missing, setMissing] = useState(false);
  const provinces = useQuery({
    queryKey: queryKey(ADMIN_TOPIC, "provinces"),
    queryFn: () => unwrap(listAdminUnits({})),
    staleTime: Number.POSITIVE_INFINITY,
  });
  const save = useWriteAction((key, body: SetWarehousePickupRequest) =>
    unwrap(setWarehousePickup({ path: { id: warehouseId }, body, headers: { "Idempotency-Key": key } })),
  );
  const set = (patch: Partial<AddressInput>) => {
    setSaved(false);
    setV((x) => ({ ...x, ...patch }));
  };
  const onSave = async () => {
    const body: SetWarehousePickupRequest = {
      version: p.version,
      name: v.name.trim(),
      phone: v.phone.trim(),
      line: v.line.trim(),
      provinceCode: v.provinceCode,
      wardCode: v.wardCode,
    };
    const empty = [body.name, body.phone, body.line, body.provinceCode, body.wardCode].some((x) => x === "");
    setMissing(empty);
    if (empty) return;
    const r = await save.run(body);
    if (!r.ok) return;
    setSaved(true);
    qc.setQueryData(pickupKey(warehouseId), r.data);
  };
  return (
    <div className="stack-sm" style={{ marginTop: 8 }}>
      <label className="field">
        <span>Người giao hàng cho hãng *</span>
        <input
          className="input"
          maxLength={120}
          value={v.name}
          onChange={(e) => {
            set({ name: e.target.value });
          }}
        />
      </label>
      <label className="field">
        <span>SĐT kho *</span>
        <input
          className="input num"
          inputMode="tel"
          maxLength={20}
          value={v.phone}
          onChange={(e) => {
            set({ phone: e.target.value });
          }}
        />
      </label>
      <label className="field">
        <span>Địa chỉ *</span>
        <input
          className="input"
          maxLength={500}
          value={v.line}
          onChange={(e) => {
            set({ line: e.target.value });
          }}
        />
      </label>
      <label className="field">
        <span>Tỉnh *</span>
        <span className="select-wrap">
          <select
            value={v.provinceCode}
            onChange={(e) => {
              set({ provinceCode: e.target.value, wardCode: "", wardName: "" });
            }}
          >
            <option value="">Chọn tỉnh/thành</option>
            {(provinces.data?.items ?? []).map((u) => (
              <option key={u.code} value={u.code}>
                {u.name}
              </option>
            ))}
          </select>
        </span>
      </label>
      <WardField value={v} onChange={set} />
      {v.wardCode !== "" && (v.wardName ?? "") === "" && (
        <span className="hint">Đang lưu xã/phường mã {v.wardCode}. Gõ tên để đổi.</span>
      )}
      {missing && <span className="field-err">Nhập đủ người giao, SĐT, địa chỉ, tỉnh, xã/phường</span>}
      <button
        type="button"
        className="btn btn-line"
        disabled={save.pending}
        onClick={() => {
          void onSave();
        }}
      >
        Lưu địa chỉ lấy hàng
      </button>
      {saved && (
        <span className="small ok-text" role="status">
          Đã lưu địa chỉ lấy hàng.
        </span>
      )}
      {save.error !== null && (
        <RefusalBox
          error={save.error}
          message={save.message}
          conflictTitle="Địa chỉ lấy hàng vừa được người khác sửa — tải lại"
          onReload={() => {
            void qc.invalidateQueries({ queryKey: pickupKey(warehouseId) });
          }}
        />
      )}
      <p className="hint">Của kho, dùng cho mọi hãng gửi từ kho này. Không phải địa chỉ khách.</p>
    </div>
  );
}

export function PickupPanel({ warehouseId, warehouseName }: { warehouseId: string; warehouseName: string }) {
  const id = useId();
  const [saved, setSaved] = useState(false);
  const q = useQuery({
    queryKey: pickupKey(warehouseId),
    queryFn: () => unwrap(getWarehousePickup({ path: { id: warehouseId } })),
  });
  return (
    <section className="card" aria-labelledby={id}>
      <h2 id={id}>{`Địa chỉ lấy hàng · ${warehouseName}`}</h2>
      <QueryState q={q} />
      {q.data !== undefined && (
        <PickupForm key={q.data.version} p={q.data} warehouseId={warehouseId} saved={saved} setSaved={setSaved} />
      )}
    </section>
  );
}
