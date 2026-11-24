// Thẻ "Hãng trên website" ở cuối màn Kết nối vận chuyển (kế hoạch M5 Task 208, M5-25; mockup
// docs/mockups/m5/web-carriers.html trạng thái A, B, C; HV09–HV12). Bật hiện và thứ tự là cờ showOnWeb, webSort của
// từng tài khoản hãng (updateCarrierAccount của M3, lưu ngay từng tài khoản); hãng chọn sẵn và phí dự phòng có nút Lưu
// riêng (saveWebCarrierSettings của Task 198, giữ version). Số vận đơn 30 ngày lấy từ getWebCarrierSettings. Mục menu
// "Hãng trên web" (nhóm Website) mở chính màn Kết nối vận chuyển và cuộn tới thẻ này (focus).
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useRef, useState } from "react";
import {
  getWebCarrierSettings,
  saveWebCarrierSettings,
  updateCarrierAccount,
  type CarrierAccount,
  type SaveWebCarrierSettingsRequest,
  type WebCarrierSettings,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction, type WriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { accountsKey } from "../carriers/carrierData";
import { CARRIER_ACCOUNT_TOPIC, carrierName } from "../carriers/carrierRules";
import { formatMoney } from "../products/productRules";
import {
  blockReason,
  formOf,
  isLastActive,
  nextWebSort,
  readiness,
  reorder,
  sameForm,
  toRequest,
  validateForm,
  webRows,
  type WebForm,
} from "./webCarrierRules";
import "./web-carriers.css";

/** Cài đặt hãng trên web: chủ đề của tài khoản hãng, nên báo hiệu tài khoản đổi (bật, ẩn, đổi thứ tự) tải lại số. */
export const WEB_SETTINGS_KEY = queryKey(CARRIER_ACCOUNT_TOPIC, "web-settings");

/** Một lệnh ghi cờ hiện trên web / thứ tự của một tài khoản (thân PATCH kèm version đã đọc). */
interface AccountWrite {
  id: string;
  version: number;
  showOnWeb?: boolean;
  webSort?: number;
}

const isConflict = (e: unknown) => e instanceof ApiError && e.status === 409 && e.code === "version_conflict";

type Notice = { kind: "unpreferred"; name: string } | { kind: "last" } | null;

/** Hai việc shop phải làm trước khi website dùng báo phí của CRM (Task 198): nhắc ngay đầu thẻ. */
function Readiness({ carrier, fallback }: { carrier: boolean; fallback: boolean }) {
  const missing = [carrier ? null : "hãng hiện trên website", fallback ? null : "phí dự phòng"].filter(
    (x): x is string => x !== null,
  );
  return (
    <div
      className={missing.length === 0 ? "box box-info" : "box box-err"}
      role="note"
      aria-label="Trước khi website dùng báo phí của CRM"
    >
      <p>
        <b>Trước khi website dùng báo phí của CRM</b>, shop cần bật “Hiện trên website” cho ít nhất một hãng đang hoạt
        động và nhập phí dự phòng (khách vẫn đặt được khi không hãng nào báo được phí).
      </p>
      <p className="small">{missing.length === 0 ? "Đã đủ hai việc này." : `Còn thiếu: ${missing.join(", ")}.`}</p>
    </div>
  );
}

function Row({
  a,
  order,
  shown,
  count,
  preferred,
  manage,
  busy,
  saving,
  first,
  last,
  onToggle,
  onMove,
}: {
  a: CarrierAccount;
  order: number | null;
  shown: boolean;
  count: number | undefined;
  preferred: boolean;
  manage: boolean;
  busy: boolean;
  saving: boolean;
  first: boolean;
  last: boolean;
  onToggle: (a: CarrierAccount, on: boolean) => void;
  onMove: (dir: -1 | 1) => void;
}) {
  const reason = blockReason(a);
  // Không bật được khi không hoạt động; tài khoản không hoạt động mà vẫn đang hiện thì vẫn ẩn được.
  const locked = reason !== null && !shown;
  const carrier = carrierName(a.carrierKey);
  let line;
  if (saving) line = <span className="small muted">Đang lưu…</span>;
  else if (locked) line = <span className={a.status === "auth_failed" ? "small err-text" : "small"}>{reason}</span>;
  else {
    const parts = [carrier, a.status === "active" ? "Đang hoạt động" : "Không hoạt động: khách không thấy"];
    if (count !== undefined) parts.push(`${String(count)} vận đơn trong 30 ngày`);
    line = (
      <span className="small muted">
        {parts.join(" · ")}
        {preferred && (
          <>
            {" · "}
            <b>Chọn sẵn</b>
          </>
        )}
      </span>
    );
  }
  return (
    <div className={locked ? "oitem is-off" : "oitem"}>
      <span className="ord">{order === null ? "–" : String(order)}</span>
      <div className="grow">
        <b className="wrap-any">{a.name}</b>
        <br />
        {line}
      </div>
      {manage && (
        <label className={locked || busy ? "sw is-off" : "sw"}>
          <input
            type="checkbox"
            aria-label={`Hiện trên website: ${a.name}`}
            checked={shown}
            disabled={locked || busy}
            onChange={(e) => {
              onToggle(a, e.target.checked);
            }}
          />{" "}
          Hiện trên website
        </label>
      )}
      {manage && shown && (
        <div className="row">
          <button
            type="button"
            className="ibtn"
            aria-label={`Đưa ${a.name} lên`}
            disabled={first || busy}
            onClick={() => {
              onMove(-1);
            }}
          >
            ↑
          </button>
          <button
            type="button"
            className="ibtn"
            aria-label={`Đưa ${a.name} xuống`}
            disabled={last || busy}
            onClick={() => {
              onMove(1);
            }}
          >
            ↓
          </button>
        </div>
      )}
    </div>
  );
}

/** Bản nháp của form và version của bản cài đặt nó dựa vào (gửi đúng version đã đọc, không lấy bản vừa tải lại ngầm). */
interface Draft {
  form: WebForm;
  version: number;
}

function SettingsForm({
  data,
  choices,
  busy,
  save,
  onSaved,
  onConflict,
}: {
  data: WebCarrierSettings;
  choices: readonly CarrierAccount[];
  busy: boolean;
  save: WriteAction<SaveWebCarrierSettingsRequest, WebCarrierSettings>;
  onSaved: () => void;
  onConflict: () => void;
}) {
  const [draft, setDraft] = useState<Draft | null>(null);
  const base = formOf(data);
  const f = draft?.form ?? base;
  const dirty = draft !== null && !sameForm(draft.form, base);
  const errs = validateForm(
    f,
    choices.map((a) => a.id),
  );
  const bad = Object.keys(errs).length > 0;
  const edit = (p: Partial<WebForm>) => {
    save.reset();
    setDraft({ form: { ...f, ...p }, version: draft?.version ?? data.version });
  };
  const submit = async () => {
    const r = await save.run(toRequest(f, draft?.version ?? data.version));
    if (r.ok) {
      setDraft(null);
      onSaved();
    } else if (isConflict(r.error)) {
      setDraft(null);
      save.reset();
      onConflict();
    }
  };
  const disabled = busy;
  return (
    <div className="stack wc-form">
      <fieldset className="stack-sm">
        <legend className="label">Hãng chọn sẵn ở bước đặt hàng</legend>
        <div className="radios">
          <label className="check">
            <input
              type="radio"
              name="web-preferred"
              checked={f.preferred === ""}
              disabled={disabled}
              onChange={() => {
                edit({ preferred: "" });
              }}
            />
            Không chọn sẵn (website chọn hãng rẻ nhất)
          </label>
          {choices.map((a) => (
            <label key={a.id} className="check">
              <input
                type="radio"
                name="web-preferred"
                checked={f.preferred === a.id}
                disabled={disabled}
                onChange={() => {
                  edit({ preferred: a.id });
                }}
              />
              {a.name}
            </label>
          ))}
        </div>
        {errs.preferred === undefined ? (
          <p className="hint">
            Chỉ chọn được hãng đang hiện trên website. Hãng chọn sẵn đứng đầu và được tích trước; khách vẫn đổi được.
          </p>
        ) : (
          <p className="hint err-text">{errs.preferred}</p>
        )}
      </fieldset>
      <fieldset className="stack-sm">
        <legend className="label">Phí dự phòng</legend>
        <label className="check">
          <input
            type="checkbox"
            checked={f.useFallback}
            disabled={disabled}
            onChange={(e) => {
              edit({ useFallback: e.target.checked });
            }}
          />
          Dùng phí dự phòng khi không hãng nào báo được phí
        </label>
        <div className="unit">
          <input
            className="input"
            inputMode="numeric"
            placeholder="30.000"
            aria-label="Phí dự phòng"
            aria-invalid={errs.fee !== undefined}
            value={f.fee}
            disabled={disabled || !f.useFallback}
            onChange={(e) => {
              edit({ fee: e.target.value });
            }}
          />
          <span>₫ · 0–500.000</span>
        </div>
        {errs.fee === undefined ? (
          <p className="hint">
            Mặc định tắt: hãng nào cũng không báo được phí thì khách thấy &quot;Chưa tính được phí giao hàng, thử lại
            sau&quot; và chưa đặt được. Bật thì khách thấy phí này ghi &quot;ước tính&quot;.
          </p>
        ) : (
          <p className="hint err-text">{errs.fee}</p>
        )}
      </fieldset>
      {save.message !== null && (
        <div className="box box-err" role="alert">
          {save.message}
        </div>
      )}
      <div className="savebar">
        <span className="small muted grow">{dirty ? "Có thay đổi chưa lưu." : "Chưa có thay đổi."}</span>
        <button
          type="button"
          className="btn btn-quiet"
          disabled={!dirty || save.pending}
          onClick={() => {
            save.reset();
            setDraft(null);
          }}
        >
          Bỏ thay đổi
        </button>
        <button
          type="button"
          className="btn btn-dark"
          disabled={!dirty || bad || disabled}
          onClick={() => {
            void submit();
          }}
        >
          {save.pending ? "Đang lưu…" : "Lưu"}
        </button>
      </div>
    </div>
  );
}

/** Chỉ xem (thiếu quyền ghi): hãng chọn sẵn, phí dự phòng bằng chữ (mockup C). */
function ReadOnlySettings({ data, accounts }: { data: WebCarrierSettings; accounts: readonly CarrierAccount[] }) {
  const pref = accounts.find((a) => a.id === data.preferredAccountId);
  return (
    <div className="stack-sm wc-form">
      <p className="small">
        {pref === undefined ? "Không chọn sẵn (website chọn hãng rẻ nhất)" : `Hãng chọn sẵn: ${pref.name}`}
        <br />
        {data.fallbackFee === undefined ? "Phí dự phòng: tắt" : `Phí dự phòng: ${formatMoney(data.fallbackFee)}`}
      </p>
      <p className="reason">
        Bạn chỉ có quyền xem (thiếu quyền &quot;Quản lý tài khoản hãng&quot;); không bật, tắt, đổi thứ tự được.
      </p>
    </div>
  );
}

export function WebCarriers({
  accounts,
  manage,
  focus,
}: {
  accounts: readonly CarrierAccount[];
  manage: boolean;
  focus: boolean;
}) {
  const qc = useQueryClient();
  const titleId = useId();
  const ref = useRef<HTMLElement>(null);
  const settings = useQuery({ queryKey: WEB_SETTINGS_KEY, queryFn: () => unwrap(getWebCarrierSettings()) });
  const [notice, setNotice] = useState<Notice>(null);
  const [conflict, setConflict] = useState(false);
  const [savingIds, setSavingIds] = useState<readonly string[]>([]);
  const save = useWriteAction((key: string, body: SaveWebCarrierSettingsRequest) =>
    unwrap(saveWebCarrierSettings({ body, headers: { "Idempotency-Key": key } })),
  );
  const patch = useWriteAction(async (key: string, body: { items: AccountWrite[] }) => {
    // Nhiều tài khoản (đổi thứ tự): mỗi tài khoản một khóa con của cùng thao tác; gửi lại thao tác dùng lại đúng khóa.
    for (const [i, { id, ...fields }] of body.items.entries()) {
      const k = body.items.length === 1 ? key : `${key}-${String(i)}`;
      await unwrap(updateCarrierAccount({ path: { id }, body: fields, headers: { "Idempotency-Key": k } }));
    }
  });

  useEffect(() => {
    if (focus) ref.current?.scrollIntoView({ block: "start" });
  }, [focus]);

  const reload = () => {
    void qc.invalidateQueries({ queryKey: accountsKey });
    void qc.invalidateQueries({ queryKey: WEB_SETTINGS_KEY });
  };

  const { shown, hidden } = webRows(accounts);
  const data = settings.data;
  const counts = new Map((data?.carriers ?? []).map((c) => [c.accountId, c.shipments30d]));
  const choices = shown.filter((a) => a.status === "active");
  // Một lần ghi bất kỳ của thẻ (bật/ẩn, thứ tự, lưu chọn sẵn và phí) đang chạy → khóa mọi thao tác ghi khác.
  const busy = patch.pending || save.pending;

  const write = async (items: AccountWrite[], after?: () => void) => {
    setNotice(null);
    setConflict(false);
    setSavingIds(items.map((w) => w.id));
    const r = await patch.run({ items });
    setSavingIds([]);
    reload();
    if (r.ok) after?.();
    else if (isConflict(r.error)) {
      patch.reset();
      setConflict(true);
    }
  };

  const onToggle = (a: CarrierAccount, on: boolean) => {
    if (!on && isLastActive(shown, a)) {
      setNotice({ kind: "last" });
      return;
    }
    if (on) {
      void write([{ id: a.id, version: a.version, showOnWeb: true, webSort: nextWebSort(shown) }]);
      return;
    }
    const wasPreferred = data?.preferredAccountId === a.id;
    void write([{ id: a.id, version: a.version, showOnWeb: false }], () => {
      if (wasPreferred) setNotice({ kind: "unpreferred", name: a.name });
    });
  };

  // Câu trống chỉ khi không tài khoản nào hoạt động và không tài khoản nào đang hiện (còn thì vẽ để ẩn được).
  const noActive = shown.length === 0 && !accounts.some((a) => a.status === "active");
  const ready = readiness(shown, data);

  return (
    <section className="card webcar" aria-labelledby={titleId} ref={ref}>
      <div className="row-between">
        <div>
          <h2 id={titleId}>Hãng trên website</h2>
          <p className="small muted">
            Bật hiện, đổi thứ tự là lưu ngay từng tài khoản. Thứ tự là thứ tự khách thấy ở bước đặt hàng.
          </p>
        </div>
        {data !== undefined && !noActive && <span className="chip chip-ok">{choices.length} hãng đang hiện</span>}
      </div>

      {settings.isError && (
        <div className="box box-err" role="alert">
          {errorMessage(settings.error)}
        </div>
      )}
      {settings.isPending && <p className="muted">Đang tải…</p>}
      {data !== undefined && noActive && (
        <div className="empty">
          <h2>Chưa có tài khoản hãng nào hoạt động</h2>
          <p className="muted">
            Website chưa có hãng để khách chọn. Thêm và kiểm tra tài khoản hãng ở phần Kết nối vận chuyển phía trên.
          </p>
        </div>
      )}
      {data !== undefined && !noActive && (
        <>
          <Readiness carrier={ready.carrier} fallback={ready.fallback} />
          {conflict && (
            <div className="box box-err stack-sm" role="alert">
              <b>Người khác vừa đổi phần này.</b>
              <p>Đã tải lại. Xem lại rồi chọn lại.</p>
              <div>
                <button
                  type="button"
                  className="btn btn-line"
                  onClick={() => {
                    setConflict(false);
                  }}
                >
                  Đã hiểu
                </button>
              </div>
            </div>
          )}
          {patch.message !== null && (
            <div className="box box-err" role="alert">
              {patch.message}
            </div>
          )}
          <div className="olist">
            {[...shown, ...hidden].map((a, i) => {
              const isShown = i < shown.length;
              return (
                <Row
                  key={a.id}
                  a={a}
                  order={isShown ? i + 1 : null}
                  shown={isShown}
                  count={counts.get(a.id)}
                  preferred={data.preferredAccountId === a.id}
                  manage={manage}
                  busy={busy}
                  saving={savingIds.includes(a.id)}
                  first={i === 0}
                  last={i === shown.length - 1}
                  onToggle={onToggle}
                  onMove={(dir) => {
                    const items = reorder(shown, i, dir);
                    if (items.length > 0) void write(items);
                  }}
                />
              );
            })}
          </div>
          {notice?.kind === "unpreferred" && (
            <div className="box box-info">
              Đã ẩn {notice.name}. Hãng này đang là hãng chọn sẵn nên <b>đã bỏ chọn sẵn</b>: website chọn hãng rẻ nhất.
            </div>
          )}
          {notice?.kind === "last" && (
            <div className="box box-err" role="alert">
              <b>Phải hiện ít nhất 1 hãng</b>, nếu không khách không đặt được hàng trên website.
            </div>
          )}
          {manage ? (
            <SettingsForm
              data={data}
              choices={choices}
              busy={busy}
              save={save}
              onSaved={reload}
              onConflict={() => {
                setConflict(true);
                reload();
              }}
            />
          ) : (
            <ReadOnlySettings data={data} accounts={accounts} />
          )}
        </>
      )}
    </section>
  );
}
