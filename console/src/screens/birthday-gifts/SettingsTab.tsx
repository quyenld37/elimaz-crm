// Tab "Cài đặt quà" của màn Quà sinh nhật (mockup trạng thái C): bật/tắt, mức giảm, đơn tối thiểu, gửi trước, dùng
// trong, lưu ngày sinh trước ít nhất; lỗi dưới ô theo giới hạn của saveBirthdaySettings; xem trước câu khách thấy và
// tin mẫu; lưu kèm version (409 version_conflict → tải lại bản mới, bỏ bản nháp).
import { useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { useState } from "react";
import { saveBirthdaySettings, type BirthdaySettings, type SaveBirthdaySettingsRequest } from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { shortTime } from "../../app/time";
import { ErrorBox } from "./BirthdayParts";
import { headers, reloadBirthday, settingsKey } from "./birthdayData";
import {
  giftMessage,
  readSettings,
  settingsDraft,
  thanksText,
  type SettingsDraft,
  type SettingsErrors,
} from "./birthdayRules";

const DAY_MS = 86_400_000;

type Field = keyof SettingsErrors;
const FIELDS: readonly { key: Field; label: string; unit: string; hint: string }[] = [
  { key: "amount", label: "Mức giảm", unit: "₫", hint: "10.000–1.000.000 ₫, bội số của 1.000" },
  { key: "minOrder", label: "Đơn tối thiểu", unit: "₫", hint: "0 = đơn nào cũng dùng được" },
  { key: "daysBefore", label: "Gửi trước sinh nhật", unit: "ngày", hint: "0–30; CRM tạo mã trong khung 9:00–21:00" },
  { key: "validDays", label: "Dùng trong", unit: "ngày", hint: "1–60 ngày kể từ lúc tạo mã" },
  {
    key: "minLeadDays",
    label: "Lưu ngày sinh trước ít nhất",
    unit: "ngày",
    hint: "0–120; chống khách vừa lưu đã nhận quà",
  },
];

function Preview({ s }: { s: Pick<BirthdaySettings, "amount" | "minOrder" | "daysBefore" | "validDays"> }) {
  const [from] = useState(() => Date.now());
  const sample = giftMessage({
    fullName: "Nguyễn Thị Lan",
    phone: "+84912345678",
    code: "SN4E1C95",
    amount: s.amount,
    minOrder: s.minOrder,
    validFrom: new Date(from).toISOString(),
    validTo: new Date(from + s.validDays * DAY_MS).toISOString(),
  });
  return (
    <section className="card stack-sm">
      <h3>Khách thấy trên trang cảm ơn</h3>
      <p className="note-box">{thanksText(s)}</p>
      <h3>Tin nhắn gửi khách (mẫu)</h3>
      <p className="note-box bg-pre">{sample}</p>
    </section>
  );
}

function Form(o: { data: BirthdaySettings; canManage: boolean; onToast: (t: string | null) => void }) {
  const qc = useQueryClient();
  const [draft, setDraft] = useState<SettingsDraft | null>(null);
  const [conflict, setConflict] = useState(false);
  const save = useWriteAction((key: string, body: SaveBirthdaySettingsRequest) =>
    unwrap(saveBirthdaySettings({ body, headers: headers(key) })),
  );
  const base = settingsDraft(o.data);
  const cur = draft ?? base;
  const { fields, errors } = readSettings(cur);
  const dirty = draft !== null && JSON.stringify(draft) !== JSON.stringify(base);
  const nErr = Object.keys(errors).length;
  const ro = !o.canManage;

  const set = (p: Partial<SettingsDraft>) => {
    setDraft({ ...cur, ...p });
  };
  const onSave = () => {
    if (fields === null) return;
    o.onToast(null);
    setConflict(false);
    void save.run({ ...fields, version: o.data.version }).then((r) => {
      if (r.ok) {
        qc.setQueryData(settingsKey, r.data);
        setDraft(null);
        o.onToast("Đã lưu cài đặt quà.");
        reloadBirthday(qc);
      } else if (r.error instanceof ApiError && r.error.code === "version_conflict") {
        save.reset();
        setConflict(true);
        setDraft(null);
        reloadBirthday(qc);
      }
    });
  };

  return (
    <div className="cols">
      <section className="card stack">
        <label className="check">
          <input
            type="checkbox"
            checked={cur.enabled}
            disabled={ro}
            onChange={(e) => {
              set({ enabled: e.target.checked });
            }}
          />{" "}
          <b>Bật quà sinh nhật</b>
        </label>
        <p className="hint">
          Tắt thì trang cảm ơn của website thôi hỏi ngày sinh và CRM ngừng tạo mã mới. Mã đã tạo vẫn dùng được tới hạn.
        </p>
        <div className="set-g">
          {FIELDS.map((f) => (
            <label key={f.key} className="field">
              <span>{f.label}</span>
              <span className="unit">
                <input
                  className={errors[f.key] === undefined ? "input" : "input is-err"}
                  aria-label={f.label}
                  inputMode="numeric"
                  value={cur[f.key]}
                  disabled={ro}
                  onChange={(e) => {
                    set({ [f.key]: e.target.value });
                  }}
                />
                <span>{f.unit}</span>
              </span>
              {errors[f.key] === undefined ? (
                <span className="hint">{f.hint}</span>
              ) : (
                <span className="field-err">{errors[f.key]}</span>
              )}
            </label>
          ))}
        </div>
        <p className="small">
          Chỉ tặng khách đã nhận thành công ít nhất 1 đơn đặt trên website. Khách chưa có đơn giao thành công chờ tới
          khi có đơn trước sinh nhật.
        </p>
        <p className="small muted">
          Áp dụng cho mã tạo từ lần chạy sau; mã đã tạo giữ nguyên.
          {o.data.updatedAt !== undefined &&
            ` Sửa lần cuối ${shortTime(o.data.updatedAt)}${o.data.updatedByName === undefined ? "" : ` · ${o.data.updatedByName}`}.`}
        </p>
        {!ro && (
          <div className="savebar">
            <span className="small err-text grow">{nErr > 0 ? `Sửa ${String(nErr)} lỗi để lưu.` : ""}</span>
            <button
              type="button"
              className="btn btn-quiet"
              disabled={!dirty || save.pending}
              onClick={() => {
                setDraft(null);
              }}
            >
              Hủy thay đổi
            </button>
            <button
              type="button"
              className="btn btn-dark"
              disabled={!dirty || fields === null || save.pending}
              onClick={onSave}
            >
              {save.pending ? "Đang lưu…" : "Lưu cài đặt"}
            </button>
          </div>
        )}
      </section>
      <aside className="stack">
        <Preview s={fields ?? o.data} />
        {conflict && (
          <div className="box box-err stack-sm">
            <b>Cài đặt vừa được người khác đổi.</b>
            <p>Đã tải lại bản mới. Nhập lại thay đổi của bạn rồi lưu.</p>
          </div>
        )}
        {save.message !== null && <div className="box box-err">{save.message}</div>}
      </aside>
    </div>
  );
}

export function SettingsTab(o: {
  settings: UseQueryResult<BirthdaySettings>;
  canManage: boolean;
  onToast: (t: string | null) => void;
}) {
  if (o.settings.isError) return <ErrorBox error={o.settings.error} />;
  if (o.settings.data === undefined) return null;
  return <Form data={o.settings.data} canManage={o.canManage} onToast={o.onToast} />;
}
