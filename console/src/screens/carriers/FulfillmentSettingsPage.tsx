// Cài đặt vận chuyển /settings/fulfillment (kế hoạch M3 Task 132; mockup docs/mockups/m3/ket-noi-van-chuyen.html trạng
// thái F; HV06): ngưỡng đơn kẹt theo trạng thái, hạn gọi lại, quá hạn tiền, giữ hàng quá hạn, khách hay hoàn (tỷ lệ theo
// %), cân nặng mặc định, hạn báo phí. Chu kỳ tra trạng thái không hiện: gửi lại nguyên giá trị đang lưu. Ghi cả bộ kèm
// version (409 → tải lại); máy chủ kiểm khoảng (422 setting_out_of_range nêu tên trường → báo dưới đúng ô).
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  getFulfillmentSettings,
  updateFulfillmentSettings,
  type FulfillmentSettings,
  type UpdateFulfillmentSettingsRequest,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { RefusalBox } from "../staff/staffData";
import {
  ALL_ROWS,
  DEADLINE_ROWS,
  DEFAULT_SETTINGS,
  FULFILLMENT_SETTINGS_KEY,
  OTHER_ROWS,
  RISK_ROWS,
  STUCK_ROWS,
  readSetting,
  readSettings,
  rowOfServerField,
  settingInputs,
  type SettingInputs,
  type SettingRow,
} from "./carrierRules";
import "./carriers.css";

const settingsKey = queryKey(FULFILLMENT_SETTINGS_KEY);

function Cell({
  row,
  value,
  error,
  onChange,
}: {
  row: SettingRow;
  value: string;
  error: string | undefined;
  onChange: (v: string) => void;
}) {
  return (
    <div className="field">
      <span aria-hidden="true">{row.label}</span>
      <span className="unit">
        <input
          className={error === undefined ? "input" : "input is-err"}
          inputMode={row.unit === "%" ? "decimal" : "numeric"}
          aria-label={`${row.label} (${row.unit})`}
          {...(error === undefined ? {} : { "aria-invalid": true })}
          value={value}
          onChange={(e) => {
            onChange(e.target.value);
          }}
        />
        <span>{row.unit}</span>
      </span>
      {error !== undefined && <span className="field-err">{error}</span>}
    </div>
  );
}

interface SettingsFormProps {
  s: FulfillmentSettings;
  /** "Đã lưu" giữ ở trang: form dựng lại theo version mới sau khi lưu. */
  saved: boolean;
  setSaved: (v: boolean) => void;
}

function SettingsForm({ s, saved, setSaved }: SettingsFormProps) {
  const qc = useQueryClient();
  const initial = settingInputs(s);
  const [inputs, setInputs] = useState<SettingInputs>(initial);
  const [serverErr, setServerErr] = useState<Record<string, string>>({});
  const save = useWriteAction((key, body: UpdateFulfillmentSettingsRequest) =>
    unwrap(updateFulfillmentSettings({ body, headers: { "Idempotency-Key": key } })),
  );
  const localErr: Record<string, string> = {};
  for (const r of ALL_ROWS) {
    const v = readSetting(r.unit, inputs[r.path] ?? "");
    if (typeof v === "string") localErr[r.path] = v;
  }
  const errors = { ...serverErr, ...localErr };
  const changed = ALL_ROWS.some((r) => inputs[r.path] !== initial[r.path]);
  const set = (path: string, v: string) => {
    setSaved(false);
    setServerErr((e) => Object.fromEntries(Object.entries(e).filter(([k]) => k !== path)));
    setInputs((x) => ({ ...x, [path]: v }));
  };
  const onSave = async () => {
    const { body } = readSettings(s, inputs);
    if (body === null) return;
    const r = await save.run(body);
    if (!r.ok) {
      const e = r.error;
      if (e instanceof ApiError && e.code === "setting_out_of_range") {
        const field = typeof e.details.field === "string" ? e.details.field : "";
        const row = rowOfServerField(field);
        if (row !== undefined) setServerErr({ [row.path]: "Ngoài khoảng cho phép" });
      }
      return;
    }
    setSaved(true);
    qc.setQueryData(settingsKey, r.data);
  };
  const cell = (r: SettingRow) => (
    <Cell
      key={r.path}
      row={r}
      value={inputs[r.path] ?? ""}
      error={errors[r.path]}
      onChange={(v) => {
        set(r.path, v);
      }}
    />
  );
  const conflict = save.error instanceof ApiError && save.error.code === "version_conflict";
  const fieldErr = Object.keys(serverErr).length > 0;
  return (
    <section className="card" aria-label="Các ngưỡng">
      <div className="row-between">
        <h2>Cài đặt vận chuyển</h2>
        {s.version === 0 && <span className="small muted">Đang dùng mặc định (chưa lưu lần nào)</span>}
      </div>
      <div className="stack" style={{ marginTop: 16 }}>
        <div>
          <h3>Đơn kẹt: hãng không cập nhật quá</h3>
          <div className="set-g" style={{ marginTop: 8 }}>
            {STUCK_ROWS.map(cell)}
          </div>
        </div>
        <div className="set-g">{DEADLINE_ROWS.map(cell)}</div>
        <div>
          <h3>Khách hay hoàn</h3>
          <div className="set-g" style={{ marginTop: 8 }}>
            {RISK_ROWS.map(cell)}
          </div>
        </div>
        <div className="set-g">{OTHER_ROWS.map(cell)}</div>
      </div>
      <div className="savebar" style={{ marginTop: 16 }}>
        <button
          type="button"
          className="btn btn-quiet"
          onClick={() => {
            setServerErr({});
            setSaved(false);
            setInputs(settingInputs({ ...DEFAULT_SETTINGS, pollMinutes: s.pollMinutes, version: s.version }));
          }}
        >
          Về mặc định
        </button>
        <button
          type="button"
          className="btn btn-dark"
          disabled={!changed || Object.keys(localErr).length > 0 || save.pending}
          onClick={() => {
            void onSave();
          }}
        >
          Lưu cài đặt
        </button>
      </div>
      {saved && (
        <p className="small ok-text" role="status">
          Đã lưu cài đặt.
        </p>
      )}
      {conflict ? (
        <RefusalBox
          error={save.error}
          message={save.message}
          conflictTitle="Cài đặt vừa được người khác sửa — tải lại"
          onReload={() => {
            void qc.invalidateQueries({ queryKey: settingsKey });
          }}
        />
      ) : (
        save.error !== null && !fieldErr && <RefusalBox error={save.error} message={save.message} />
      )}
      <p className="hint">
        Mọi số phải lớn hơn 0. Chu kỳ tra trạng thái theo từng trạng thái để ở mặc định (máy chủ giữ), không hiện trên
        màn này.
      </p>
    </section>
  );
}

export function FulfillmentSettingsPage() {
  const q = useQuery({ queryKey: settingsKey, queryFn: () => unwrap(getFulfillmentSettings()) });
  const [saved, setSaved] = useState(false);
  return (
    <div className="carriers">
      <div className="page-h">
        <div className="grow">
          <p className="crumb">Vận chuyển / Cài đặt vận chuyển</p>
          <h1>Cài đặt vận chuyển</h1>
          <p className="sub">Ngưỡng của các danh sách vận hành, mức khách hay hoàn, cân nặng và hạn báo phí.</p>
        </div>
      </div>
      {q.isError && (
        <div className="box box-err" role="alert">
          {errorMessage(q.error)}
        </div>
      )}
      {q.isPending && <p className="muted">Đang tải…</p>}
      {q.data !== undefined && <SettingsForm key={q.data.version} s={q.data} saved={saved} setSaved={setSaved} />}
    </div>
  );
}
