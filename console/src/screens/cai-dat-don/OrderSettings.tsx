// Cài đặt đơn /cai-dat/don (kế hoạch M2 Task M2-30; mockup docs/mockups/m2/cai-dat-don.html trạng thái A, B). Bốn cài
// đặt với giá trị hiện hành và giới hạn; máy chủ kiểm khoảng (422 setting_out_of_range nêu tên trường → báo dưới đúng
// ô), version cũ → 409 "đã thay đổi, tải lại". API chưa trả người và lúc sửa gần nhất nên màn chưa hiện dòng đó.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { updateOrderSettings, type OrderSettings as Settings, type UpdateOrderSettingsRequest } from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { ORDER_TOPIC, useOrderSettings } from "../don/orderData";
import { RefusalBox } from "../staff/staffData";
import {
  SETTING_ROWS,
  UNIT_LABEL,
  outOfRangeText,
  readSettings,
  settingInputs,
  type SettingField,
  type SettingInputs,
} from "./settingsRules";
import { TrafficSources } from "./TrafficSources";
import "./cai-dat-don.css";

const UNIT_TITLE = { hours: "Giờ", percent: "%", days: "Ngày" } as const;
const settingsKey = queryKey(ORDER_TOPIC, "settings");

function SettingsForm({ s, onSaved }: { s: Settings; onSaved: () => void }) {
  const qc = useQueryClient();
  const [inputs, setInputs] = useState<SettingInputs>(() => settingInputs(s));
  const [errors, setErrors] = useState<Partial<Record<SettingField, string>>>({});
  const save = useWriteAction((key, body: UpdateOrderSettingsRequest) =>
    unwrap(updateOrderSettings({ body, headers: { "Idempotency-Key": key } })),
  );

  const onSave = async () => {
    const r = readSettings(inputs);
    setErrors(r.errors);
    if (r.values === null) return;
    const res = await save.run({ version: s.version, ...r.values });
    if (!res.ok) {
      const e = res.error;
      if (e instanceof ApiError && e.code === "setting_out_of_range") {
        const field = typeof e.details.field === "string" ? e.details.field : "";
        const row = SETTING_ROWS.find((x) => x.field === field);
        if (row !== undefined) setErrors({ [row.field]: outOfRangeText(row.field) });
      }
      return;
    }
    onSaved();
    qc.setQueryData(settingsKey, res.data);
  };
  const conflict = save.error instanceof ApiError && save.error.code === "version_conflict";
  const fieldError =
    save.error instanceof ApiError &&
    save.error.code === "setting_out_of_range" &&
    SETTING_ROWS.some((r) => errors[r.field] !== undefined);

  return (
    <>
      <div className="card">
        {SETTING_ROWS.map((r) => {
          const err = errors[r.field];
          return (
            <div className="set" key={r.field}>
              <div>
                <h3>{r.title}</h3>
                <p className="small muted">{r.desc}</p>
              </div>
              <div className="field">
                <span>{UNIT_TITLE[r.unit]}</span>
                <input
                  className={err === undefined ? "input num" : "input num is-err"}
                  inputMode="decimal"
                  aria-label={`${r.title} (${UNIT_LABEL[r.unit]})`}
                  {...(err === undefined ? {} : { "aria-invalid": true })}
                  value={inputs[r.field]}
                  onChange={(e) => {
                    setInputs((x) => ({ ...x, [r.field]: e.target.value }));
                  }}
                />
                {err === undefined ? <span className="hint">{r.hint}</span> : <span className="field-err">{err}</span>}
              </div>
            </div>
          );
        })}
        <div className="row-between" style={{ marginTop: 8 }}>
          <span className="small muted">Áp cho đơn tạo hoặc sửa từ lúc lưu.</span>
          <button
            type="button"
            className="btn btn-dark"
            disabled={save.pending}
            onClick={() => {
              void onSave();
            }}
          >
            Lưu cài đặt
          </button>
        </div>
      </div>
      {conflict ? (
        <div className="box box-err" role="alert" style={{ marginTop: 12 }}>
          <b>Cài đặt đã thay đổi, tải lại.</b> Có người vừa lưu cài đặt đơn. Giá trị bạn nhập chưa được lưu.
          <div className="row" style={{ marginTop: 8 }}>
            <button
              type="button"
              className="btn btn-dark"
              onClick={() => {
                void qc.invalidateQueries({ queryKey: settingsKey });
              }}
            >
              Tải lại
            </button>
          </div>
        </div>
      ) : (
        save.error !== null &&
        !fieldError && (
          <div style={{ marginTop: 12 }}>
            <RefusalBox error={save.error} message={save.message} />
          </div>
        )
      )}
    </>
  );
}

export function OrderSettings() {
  const settings = useOrderSettings();
  const [saved, setSaved] = useState(false);
  const s = settings.data;
  return (
    <div className="settings-page">
      <div className="page-h">
        <div>
          <h1>Cài đặt đơn</h1>
          <p className="sub">Áp cho đơn tạo hoặc sửa từ lúc lưu; đơn cũ giữ nguyên.</p>
        </div>
      </div>
      {settings.isError && <div className="box box-err">{errorMessage(settings.error)}</div>}
      {s !== undefined && (
        <SettingsForm
          key={s.version}
          s={s}
          onSaved={() => {
            setSaved(true);
          }}
        />
      )}
      {saved && (
        <p className="small muted" role="status" style={{ marginTop: 8 }}>
          Đã lưu cài đặt.
        </p>
      )}
      <TrafficSources />
    </div>
  );
}
