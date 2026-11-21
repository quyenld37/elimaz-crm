// Hộp "Thêm tài khoản hãng" (mockup docs/mockups/m3/ket-noi-van-chuyen.html trạng thái B; VC01; HV03, HV04). Mặc định
// "Nhập mã vận đơn tay" (chạy được cả khi chưa nối API hãng, Task 448); "Nối API của hãng" có ô khóa (ô mật khẩu).
// Hãng chưa nối API → máy chủ 422 capability_not_supported, hộp báo rõ. "Lưu và kiểm tra kết nối" tạo tài khoản kèm khóa
// rồi gửi lệnh kiểm; ô khóa bị xóa khỏi state ngay khi tạo xong; kết quả kiểm về theo báo hiệu (dòng của danh sách).
import { useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { createCarrierAccount, type CarrierAccount, type CreateCarrierAccountRequest, type Warehouse } from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { Dialog, RefusalBox } from "../staff/staffData";
import { accountsKey, useAccounts, usePendingChecks, useSendCheck } from "./carrierData";
import {
  CARRIER_CHOICES,
  SELF_CARRIER,
  carrierName,
  credentialFields,
  credentialsBody,
  isChecking,
  parseWebSort,
} from "./carrierRules";

/** Lựa chọn "Cho xem hàng" của CRM (fulfillment.Options.Inspection: none | view | try); rỗng là theo hãng. */
export const INSPECTION_CHOICES = [
  { value: "", label: "Theo mặc định của hãng" },
  { value: "view", label: "Cho xem, không thử" },
  { value: "try", label: "Cho thử" },
  { value: "none", label: "Không cho xem" },
] as const;

type Env = "production" | "sandbox";

interface Draft {
  carrierKey: string;
  mode: "api" | "manual";
  name: string;
  environment: Env;
  warehouseId: string;
  externalAccountRef: string;
  defaultService: string;
  inspection: string;
  isDefault: boolean;
  showOnWeb: boolean;
  webSort: string;
}

function bodyOf(d: Draft, creds: Record<string, string> | null): CreateCarrierAccountRequest {
  const inspection = d.inspection;
  return {
    carrierKey: d.carrierKey,
    mode: d.mode,
    name: d.name.trim(),
    warehouseId: d.warehouseId,
    environment: d.environment,
    ...(d.externalAccountRef.trim() === "" ? {} : { externalAccountRef: d.externalAccountRef.trim() }),
    ...(d.defaultService.trim() === "" ? {} : { defaultService: d.defaultService.trim() }),
    ...(inspection === "" ? {} : { defaultOptions: { inspection } }),
    isDefaultForWarehouse: d.isDefault,
    showOnWeb: d.showOnWeb,
    webSort: parseWebSort(d.webSort) ?? 0,
    ...(creds === null ? {} : { credentials: creds }),
  };
}

/** Hộp kết quả kiểm sau "Lưu và kiểm tra" (bốn câu của mockup B). */
function CheckResult({ id }: { id: string }) {
  const accounts = useAccounts();
  const pending = usePendingChecks();
  const a = accounts.data?.find((x) => x.id === id);
  if (a === undefined || isChecking(a, pending[a.id])) {
    return (
      <div className="box box-info small" role="status">
        <b>Đang kiểm tra kết nối…</b> CRM đang hỏi hãng bằng khóa vừa lưu. Có thể đóng hộp này; kết quả hiện ở dòng tài
        khoản.
      </div>
    );
  }
  if (a.status === "active") {
    return (
      <div className="box box-ok small" role="status">
        <b className="ok-text">Kết nối đạt.</b> Tài khoản chuyển "Đang hoạt động". Ô khóa đã được xóa khỏi màn hình.
      </div>
    );
  }
  return (
    <div className="box box-err small" role="status">
      <b>Kết nối không đạt: hãng từ chối khóa.</b> Kiểm lại khóa trên trang {carrierName(a.carrierKey)} rồi bấm "Thay
      khóa". Tài khoản giữ trạng thái "Cần cấp quyền lại", không gửi được đơn.
    </div>
  );
}

const TEXT_NO_API =
  'Hãng này chưa nối API trong CRM. Chọn "Nhập mã vận đơn tay": tạo vận đơn trên trang của hãng rồi dán mã vào CRM.';

export function AddAccountDialog({ warehouses, onClose }: { warehouses: readonly Warehouse[]; onClose: () => void }) {
  const id = useId();
  const qc = useQueryClient();
  const sendCheck = useSendCheck();
  const [d, setD] = useState<Draft>(() => ({
    carrierKey: "vtp",
    mode: "manual",
    name: "",
    environment: "production",
    warehouseId: warehouses[0]?.id ?? "",
    externalAccountRef: "",
    defaultService: "",
    inspection: "",
    isDefault: false,
    showOnWeb: false,
    webSort: "0",
  }));
  const [creds, setCreds] = useState<Record<string, string>>({});
  const [errors, setErrors] = useState<{ name?: string; webSort?: string; warehouse?: string }>({});
  const [created, setCreated] = useState<CarrierAccount | null>(null);
  const [checkSent, setCheckSent] = useState(false);
  const create = useWriteAction((key, body: CreateCarrierAccountRequest) =>
    unwrap(createCarrierAccount({ body, headers: { "Idempotency-Key": key } })),
  );
  const check = useWriteAction((key, a: Pick<CarrierAccount, "id" | "lastCheckAt">) => sendCheck(key, a));
  const set = (p: Partial<Draft>) => {
    setD((x) => ({ ...x, ...p }));
  };
  const isSelf = d.carrierKey === SELF_CARRIER;
  const api = d.mode === "api" && !isSelf;
  const fields = credentialFields(d.carrierKey);

  const onSave = async (andCheck: boolean) => {
    const e: typeof errors = {};
    if (d.name.trim() === "") e.name = "Nhập tên tài khoản";
    if (parseWebSort(d.webSort) === null) e.webSort = "Thứ tự từ 0 đến 1000";
    if (d.warehouseId === "") e.warehouse = "Chọn kho gửi hàng";
    setErrors(e);
    if (Object.keys(e).length > 0) return;
    const body = bodyOf({ ...d, mode: api ? "api" : "manual" }, api ? credentialsBody(creds) : null);
    const r = await create.run(body);
    if (!r.ok) return;
    setCreds({});
    void qc.invalidateQueries({ queryKey: accountsKey });
    if (!andCheck) {
      onClose();
      return;
    }
    setCreated(r.data);
    const c = await check.run({
      id: r.data.id,
      ...(r.data.lastCheckAt === undefined ? {} : { lastCheckAt: r.data.lastCheckAt }),
    });
    setCheckSent(c.ok);
  };

  const noApi = create.error instanceof ApiError && create.error.code === "capability_not_supported";
  const canCheck = api && credentialsBody(creds) !== null;
  return (
    <Dialog
      title="Thêm tài khoản hãng"
      closeButton
      className="carriers-dlg"
      onClose={onClose}
      footer={
        created !== null ? (
          <button type="button" className="btn btn-dark" onClick={onClose}>
            Đóng
          </button>
        ) : (
          <>
            <button type="button" className="btn btn-quiet" onClick={onClose}>
              Hủy
            </button>
            {api ? (
              <>
                <button
                  type="button"
                  className="btn btn-line"
                  disabled={create.pending}
                  onClick={() => {
                    void onSave(false);
                  }}
                >
                  Lưu nháp
                </button>
                <button
                  type="button"
                  className="btn btn-dark"
                  disabled={create.pending || !canCheck}
                  onClick={() => {
                    void onSave(true);
                  }}
                >
                  Lưu và kiểm tra kết nối
                </button>
              </>
            ) : (
              <button
                type="button"
                className="btn btn-dark"
                disabled={create.pending}
                onClick={() => {
                  void onSave(false);
                }}
              >
                Lưu tài khoản
              </button>
            )}
          </>
        )
      }
    >
      {created !== null ? (
        <div className="stack-sm">
          <p className="wrap-any">
            Đã tạo <b>{created.name}</b>.
          </p>
          {checkSent ? (
            <CheckResult id={created.id} />
          ) : (
            check.error !== null && <RefusalBox error={check.error} message={check.message} />
          )}
        </div>
      ) : (
        <>
          <div className="field">
            <span id={`${id}-h`}>Hãng *</span>
            <div className="radios" role="radiogroup" aria-labelledby={`${id}-h`}>
              {CARRIER_CHOICES.map((c) => (
                <label className="check" key={c.key}>
                  <input
                    type="radio"
                    name={`${id}-carrier`}
                    checked={d.carrierKey === c.key}
                    onChange={() => {
                      set({ carrierKey: c.key, ...(c.key === SELF_CARRIER ? { mode: "manual" as const } : {}) });
                      setCreds({});
                    }}
                  />{" "}
                  {c.label}
                </label>
              ))}
            </div>
          </div>
          {!isSelf && (
            <div className="field">
              <span id={`${id}-m`}>Cách nối *</span>
              <div className="row" role="group" aria-labelledby={`${id}-m`}>
                <button
                  type="button"
                  className="toggle"
                  aria-pressed={d.mode === "api"}
                  onClick={() => {
                    set({ mode: "api" });
                  }}
                >
                  Nối API của hãng
                </button>
                <button
                  type="button"
                  className="toggle"
                  aria-pressed={d.mode === "manual"}
                  onClick={() => {
                    set({ mode: "manual" });
                    setCreds({});
                  }}
                >
                  Nhập mã vận đơn tay
                </button>
              </div>
              <span className="hint">
                Nhập mã tay: tạo vận đơn trên trang của hãng rồi dán mã vào CRM; trạng thái cập nhật tay.
              </span>
            </div>
          )}
          <div className="grid-f">
            <label className="field">
              <span>Tên tài khoản *</span>
              <input
                className={errors.name === undefined ? "input" : "input is-err"}
                maxLength={120}
                value={d.name}
                onChange={(e) => {
                  set({ name: e.target.value });
                }}
              />
              {errors.name !== undefined && <span className="field-err">{errors.name}</span>}
            </label>
            {api && (
              <label className="field">
                <span>Môi trường *</span>
                <span className="select-wrap">
                  <select
                    value={d.environment}
                    onChange={(e) => {
                      set({ environment: e.target.value === "sandbox" ? "sandbox" : "production" });
                    }}
                  >
                    <option value="production">Thật (production)</option>
                    <option value="sandbox">Thử (sandbox)</option>
                  </select>
                </span>
              </label>
            )}
            <label className="field">
              <span>Kho gửi hàng *</span>
              <span className="select-wrap">
                <select
                  value={d.warehouseId}
                  onChange={(e) => {
                    set({ warehouseId: e.target.value });
                  }}
                >
                  {warehouses.map((w) => (
                    <option key={w.id} value={w.id}>
                      {w.name}
                    </option>
                  ))}
                </select>
              </span>
              {errors.warehouse !== undefined && <span className="field-err">{errors.warehouse}</span>}
            </label>
            {!isSelf && (
              <label className="field">
                <span>Mã khách hàng ở hãng</span>
                <input
                  className="input num"
                  maxLength={128}
                  value={d.externalAccountRef}
                  onChange={(e) => {
                    set({ externalAccountRef: e.target.value });
                  }}
                />
              </label>
            )}
          </div>
          {api && (
            <div className="field">
              <span>Khóa API</span>
              <div className="grid-f">
                {fields.map((f) => (
                  <label className="field" key={f.key}>
                    <span>{f.label}</span>
                    <input
                      className="input secret"
                      type="password"
                      autoComplete="new-password"
                      aria-label={f.label}
                      maxLength={4096}
                      value={creds[f.key] ?? ""}
                      onChange={(e) => {
                        setCreds((v) => ({ ...v, [f.key]: e.target.value }));
                      }}
                    />
                  </label>
                ))}
              </div>
              <span className="hint">
                Khóa lưu dạng mã hóa; sau khi lưu không ai xem lại được, kể cả Chủ. Muốn đổi thì bấm "Thay khóa". Bỏ
                trống thì tài khoản ở trạng thái Nháp.
              </span>
            </div>
          )}
          <div className="grid-f">
            {api && (
              <label className="field">
                <span>Dịch vụ mặc định</span>
                <input
                  className="input"
                  maxLength={64}
                  placeholder="Mã dịch vụ của hãng, ví dụ VCN"
                  value={d.defaultService}
                  onChange={(e) => {
                    set({ defaultService: e.target.value });
                  }}
                />
              </label>
            )}
            {!isSelf && (
              <label className="field">
                <span>Cho xem hàng mặc định</span>
                <span className="select-wrap">
                  <select
                    value={d.inspection}
                    onChange={(e) => {
                      set({ inspection: e.target.value });
                    }}
                  >
                    {INSPECTION_CHOICES.map((c) => (
                      <option key={c.value} value={c.value}>
                        {c.label}
                      </option>
                    ))}
                  </select>
                </span>
              </label>
            )}
          </div>
          <label className="check">
            <input
              type="checkbox"
              checked={d.isDefault}
              onChange={(e) => {
                set({ isDefault: e.target.checked });
              }}
            />{" "}
            Mặc định cho kho này (chọn sẵn khi gửi đơn của kho)
          </label>
          <div className="row">
            <label className="check">
              <input
                type="checkbox"
                checked={d.showOnWeb}
                onChange={(e) => {
                  set({ showOnWeb: e.target.checked });
                }}
              />{" "}
              Hiện trên website
            </label>
            <label className="unit">
              <span>thứ tự</span>
              <input
                className={errors.webSort === undefined ? "input" : "input is-err"}
                inputMode="numeric"
                aria-label="Thứ tự trên web"
                value={d.webSort}
                onChange={(e) => {
                  set({ webSort: e.target.value });
                }}
              />
            </label>
          </div>
          {errors.webSort !== undefined && <span className="field-err">{errors.webSort}</span>}
          {noApi ? (
            <div className="box box-err small" role="alert">
              <b>{TEXT_NO_API}</b>
            </div>
          ) : (
            create.error !== null && <RefusalBox error={create.error} message={create.message} />
          )}
        </>
      )}
    </Dialog>
  );
}
