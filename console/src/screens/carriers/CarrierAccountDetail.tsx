// Trang sửa tài khoản hãng /settings/carriers/:id (kế hoạch M3 Task 132; mockup docs/mockups/m3/ket-noi-van-chuyen.html
// trạng thái C, D, E; HV03, HV04). Không có operation đọc một tài khoản: lấy từ danh sách (carrierData.useAccounts).
// Sửa chỉ gửi trường đã đổi kèm version (409 → tải lại); khóa chỉ "Thay khóa"; kiểm tra kết nối là việc nền, kết quả về
// theo báo hiệu; URL webhook chỉ máy chủ trả cho quyền manage; người chỉ có quyền xem thấy bảng đọc.
import { useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { Link, useParams } from "react-router";
import {
  setCarrierAccountStatus,
  updateCarrierAccount,
  type CarrierAccount,
  type SetCarrierAccountStatusRequest,
  type UpdateCarrierAccountRequest,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { shortTime } from "../../app/time";
import { useWriteAction } from "../../app/idempotency";
import { usePerms } from "../don/orderData";
import { Dialog, RefusalBox } from "../staff/staffData";
import { PERM_MANAGE } from "./CarrierAccountsPage";
import { CredentialsDialog } from "./CredentialsDialog";
import { LocationsPanel } from "./LocationsPanel";
import { PickupPanel } from "./PickupPanel";
import { accountsKey, useAccounts, usePendingChecks, useSendCheck, useWarehouses } from "./carrierData";
import { ENV_TEXT, carrierName, isChecking, lastCheckCell, parseWebSort, statusChip, webText } from "./carrierRules";
import "./carriers.css";

const TEXT_CONFLICT = "Tài khoản vừa được người khác sửa — tải lại";

interface EditDraft {
  name: string;
  environment: "production" | "sandbox";
  warehouseId: string;
  defaultService: string;
  isDefaultForWarehouse: boolean;
  showOnWeb: boolean;
  webSort: string;
}

const draftOf = (a: CarrierAccount): EditDraft => ({
  name: a.name,
  environment: a.environment,
  warehouseId: a.warehouseId,
  defaultService: a.defaultService ?? "",
  isDefaultForWarehouse: a.isDefaultForWarehouse,
  showOnWeb: a.showOnWeb,
  webSort: String(a.webSort),
});

/** Chỉ trường người dùng đổi (Bẫy Task 85); không đổi gì → null. */
function patchOf(a: CarrierAccount, d: EditDraft, webSort: number): UpdateCarrierAccountRequest | null {
  const p: UpdateCarrierAccountRequest = { version: a.version };
  if (d.name.trim() !== a.name) p.name = d.name.trim();
  if (d.environment !== a.environment) p.environment = d.environment;
  if (d.warehouseId !== a.warehouseId) p.warehouseId = d.warehouseId;
  if (d.defaultService.trim() !== (a.defaultService ?? "")) p.defaultService = d.defaultService.trim();
  if (d.isDefaultForWarehouse !== a.isDefaultForWarehouse) p.isDefaultForWarehouse = d.isDefaultForWarehouse;
  if (d.showOnWeb !== a.showOnWeb) p.showOnWeb = d.showOnWeb;
  if (webSort !== a.webSort) p.webSort = webSort;
  return Object.keys(p).length === 1 ? null : p;
}

function AccountHead({ a, chip }: { a: CarrierAccount; chip: { text: string; cls: string } }) {
  return (
    <div className="row-between">
      <h2 className="wrap-any">{a.name}</h2>
      <span className={chip.cls}>{chip.text}</span>
    </div>
  );
}

function EditCard({ a, chip }: { a: CarrierAccount; chip: { text: string; cls: string } }) {
  const qc = useQueryClient();
  const warehouses = useWarehouses();
  const [d, setD] = useState<EditDraft>(() => draftOf(a));
  const [nameErr, setNameErr] = useState(false);
  const save = useWriteAction((key, body: UpdateCarrierAccountRequest & { id: string }) => {
    const { id, ...rest } = body;
    return unwrap(updateCarrierAccount({ path: { id }, body: rest, headers: { "Idempotency-Key": key } }));
  });
  const set = (p: Partial<EditDraft>) => {
    setD((x) => ({ ...x, ...p }));
  };
  const webSort = parseWebSort(d.webSort);
  const patch = webSort === null ? null : patchOf(a, d, webSort);
  const onSave = async () => {
    const bad = d.name.trim() === "";
    setNameErr(bad);
    if (bad || patch === null) return;
    const r = await save.run({ id: a.id, ...patch });
    if (r.ok) void qc.invalidateQueries({ queryKey: accountsKey });
  };
  const whName = warehouses.nameOf(d.warehouseId) || "kho này";
  return (
    <section className="card" aria-label="Thông tin tài khoản">
      <AccountHead a={a} chip={chip} />
      <div className="grid-f">
        <label className="field">
          <span>Tên tài khoản *</span>
          <input
            className={nameErr ? "input is-err" : "input"}
            maxLength={120}
            value={d.name}
            onChange={(e) => {
              set({ name: e.target.value });
            }}
          />
          {nameErr && <span className="field-err">Nhập tên tài khoản</span>}
        </label>
        {a.mode === "api" && (
          <label className="field">
            <span>Môi trường</span>
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
          <span>Kho gửi hàng</span>
          <span className="select-wrap">
            <select
              value={d.warehouseId}
              onChange={(e) => {
                set({ warehouseId: e.target.value });
              }}
            >
              {warehouses.items.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.name}
                </option>
              ))}
            </select>
          </span>
        </label>
        {a.mode === "api" && (
          <label className="field">
            <span>Dịch vụ mặc định</span>
            <input
              className="input"
              maxLength={64}
              value={d.defaultService}
              onChange={(e) => {
                set({ defaultService: e.target.value });
              }}
            />
          </label>
        )}
      </div>
      <div className="row" style={{ marginTop: 8 }}>
        <label className="check">
          <input
            type="checkbox"
            checked={d.isDefaultForWarehouse}
            onChange={(e) => {
              set({ isDefaultForWarehouse: e.target.checked });
            }}
          />{" "}
          {`Mặc định cho ${whName}`}
        </label>
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
            className={webSort === null ? "input is-err" : "input"}
            inputMode="numeric"
            aria-label="Thứ tự trên web"
            value={d.webSort}
            onChange={(e) => {
              set({ webSort: e.target.value });
            }}
          />
        </label>
      </div>
      {webSort === null && <span className="field-err">Thứ tự từ 0 đến 1000</span>}
      <div className="savebar" style={{ marginTop: 16 }}>
        <button
          type="button"
          className="btn btn-quiet"
          disabled={patch === null}
          onClick={() => {
            setD(draftOf(a));
            save.reset();
          }}
        >
          Hủy
        </button>
        <button
          type="button"
          className="btn btn-dark"
          disabled={patch === null || save.pending}
          onClick={() => {
            void onSave();
          }}
        >
          Lưu thay đổi
        </button>
      </div>
      {save.error !== null && (
        <RefusalBox
          error={save.error}
          message={save.message}
          conflictTitle={TEXT_CONFLICT}
          onReload={() => {
            void qc.invalidateQueries({ queryKey: accountsKey });
          }}
        />
      )}
      <p className="hint">Chỉ gửi trường đã đổi, kèm phiên bản đang xem.</p>
    </section>
  );
}

function ReadCard({
  a,
  chip,
  warehouse,
}: {
  a: CarrierAccount;
  chip: { text: string; cls: string };
  warehouse: string;
}) {
  const last = lastCheckCell(a, false);
  return (
    <section className="card" aria-label="Thông tin tài khoản">
      <AccountHead a={a} chip={chip} />
      <dl className="kv" style={{ marginTop: 8 }}>
        <dt>Kho</dt>
        <dd>{a.isDefaultForWarehouse ? `${warehouse} · Mặc định` : warehouse}</dd>
        {a.mode === "api" && (
          <>
            <dt>Môi trường</dt>
            <dd>{ENV_TEXT[a.environment]}</dd>
          </>
        )}
        <dt>Lần kiểm</dt>
        <dd>{last.text}</dd>
        <dt>Hiện trên web</dt>
        <dd>{webText(a)}</dd>
      </dl>
    </section>
  );
}

function CheckCard({ a, checking }: { a: CarrierAccount; checking: boolean }) {
  const id = useId();
  const sendCheck = useSendCheck();
  const check = useWriteAction((key, x: Pick<CarrierAccount, "id" | "lastCheckAt">) => sendCheck(key, x));
  const last = lastCheckCell(a, false);
  return (
    <section className="card" aria-labelledby={id}>
      <h2 id={id}>Kiểm tra kết nối</h2>
      <p className="small">
        Lần kiểm gần nhất: <b className={last.err ? "err-text" : undefined}>{last.text}</b>
      </p>
      {checking && (
        <p className="small" role="status">
          <span className="chip">Đang kiểm tra…</span>
        </p>
      )}
      <button
        type="button"
        className="btn btn-line btn-wide"
        style={{ marginTop: 8 }}
        disabled={checking || check.pending}
        onClick={() => {
          void check.run({ id: a.id, ...(a.lastCheckAt === undefined ? {} : { lastCheckAt: a.lastCheckAt }) });
        }}
      >
        Kiểm tra kết nối
      </button>
      {check.error !== null && <RefusalBox error={check.error} message={check.message} />}
    </section>
  );
}

function capabilityLines(a: CarrierAccount): string[] | null {
  const c = a.capabilities;
  if (c === undefined) return null;
  const core = [
    c.quote && "báo phí",
    c.create && "tạo vận đơn",
    c.cancel && "hủy",
    c.track && "tra trạng thái",
    c.webhook && "nhận webhook",
  ].filter((x) => typeof x === "string");
  const lines: string[] = [];
  if (core.length > 0) lines.push(core.join(", ").replace(/^./, (s) => s.toUpperCase()));
  if (c.labels)
    lines.push(c.labelsBatch === true ? "In nhãn theo lô (một tệp PDF cho nhiều mã)" : "In nhãn từng vận đơn");
  lines.push(`Sửa địa chỉ sau khi gửi: ${c.updateAddress ? "có" : "không"}`);
  lines.push(`Bảng kê COD: ${c.statement ? "có định dạng" : "chưa có định dạng"}`);
  return lines;
}

function StatusCard({ a }: { a: CarrierAccount }) {
  const qc = useQueryClient();
  const [confirm, setConfirm] = useState(false);
  const act = useWriteAction((key, body: SetCarrierAccountStatusRequest & { id: string }) => {
    const { id, ...rest } = body;
    return unwrap(setCarrierAccountStatus({ path: { id }, body: rest, headers: { "Idempotency-Key": key } }));
  });
  const run = async (status: SetCarrierAccountStatusRequest["status"]) => {
    const r = await act.run({ id: a.id, status, version: a.version });
    setConfirm(false);
    if (r.ok) void qc.invalidateQueries({ queryKey: accountsKey });
  };
  const paused = a.status === "paused" || a.status === "disabled";
  return (
    <section className="card" aria-label="Trạng thái tài khoản">
      <h2>Trạng thái tài khoản</h2>
      <div className="acts" style={{ marginTop: 8 }}>
        <div className="act">
          {paused ? (
            <button
              type="button"
              className="btn btn-line"
              disabled={act.pending}
              onClick={() => {
                void run("active");
              }}
            >
              Bật lại
            </button>
          ) : (
            <button
              type="button"
              className="btn btn-line"
              disabled={act.pending}
              onClick={() => {
                void run("paused");
              }}
            >
              Tạm ngừng
            </button>
          )}
          <span className="reason">
            {paused ? "Báo phí, gửi đơn mới trở lại" : "Không báo phí, không gửi đơn mới, không tra trạng thái tự động"}
          </span>
        </div>
        <div className="act">
          <button
            type="button"
            className="btn btn-danger"
            disabled={act.pending}
            onClick={() => {
              setConfirm(true);
            }}
          >
            Thu hồi
          </button>
          <span className="reason">Ngừng hẳn; không bật lại được</span>
        </div>
      </div>
      {act.error !== null && (
        <RefusalBox
          error={act.error}
          message={act.message}
          conflictTitle={TEXT_CONFLICT}
          onReload={() => {
            void qc.invalidateQueries({ queryKey: accountsKey });
          }}
        />
      )}
      {confirm && (
        <Dialog
          title="Thu hồi tài khoản"
          onClose={() => {
            setConfirm(false);
          }}
          footer={
            <>
              <button
                type="button"
                className="btn btn-quiet"
                onClick={() => {
                  setConfirm(false);
                }}
              >
                Hủy
              </button>
              <button
                type="button"
                className="btn btn-danger"
                disabled={act.pending}
                onClick={() => {
                  void run("revoked");
                }}
              >
                Thu hồi hẳn
              </button>
            </>
          }
        >
          <p className="wrap-any">
            Thu hồi <b>{a.name}</b>: không gửi đơn mới, vận đơn cũ không được tra tự động nữa. Không bật lại được.
          </p>
        </Dialog>
      )}
    </section>
  );
}

function Detail({ a }: { a: CarrierAccount }) {
  const perms = usePerms();
  const warehouses = useWarehouses();
  const pending = usePendingChecks();
  const [rekey, setRekey] = useState(false);
  const manage = perms.can(PERM_MANAGE) && a.status !== "revoked";
  const checking = isChecking(a, pending[a.id]);
  const chip = checking ? { text: "Đang kiểm tra…", cls: "chip" } : statusChip(a);
  const warehouse = warehouses.nameOf(a.warehouseId);
  const caps = capabilityLines(a);
  const api = a.mode === "api";
  return (
    <>
      {a.status === "auth_failed" && (
        <div className="box box-err small" style={{ marginBottom: 16 }}>
          <b>Cần cấp quyền lại.</b>{" "}
          {a.lastCheckAt === undefined
            ? "Hãng từ chối khóa."
            : `Lần kiểm ${shortTime(a.lastCheckAt).replace(" ", " · ")}: hãng từ chối khóa.`}{" "}
          Không báo phí, không gửi đơn mới bằng tài khoản này; CRM không tự chuyển sang tài khoản khác. Vận đơn đã gửi
          không được tra tự động tới khi thay khóa đạt; theo dõi trên trang hãng hoặc trong "Đơn kẹt".
        </div>
      )}
      {a.status === "revoked" && (
        <div className="box box-info small" style={{ marginBottom: 16 }}>
          <b className="wrap-any">{`${a.name} · Đã thu hồi`}</b>. Không gửi đơn mới. Vận đơn cũ không được tra tự động
          nữa; theo dõi trên trang hãng.
        </div>
      )}
      <div className="cols">
        <div className="stack">
          {manage ? (
            <EditCard key={a.version} a={a} chip={chip} />
          ) : (
            <ReadCard a={a} chip={chip} warehouse={warehouse} />
          )}
          {!perms.can(PERM_MANAGE) && (
            <p className="hint">
              Bạn chỉ có quyền xem kết nối vận chuyển. Sửa, thay khóa, URL webhook cần quyền "Quản lý kết nối vận
              chuyển" (Chủ, Quản lý).
            </p>
          )}
          {api && manage && (
            <section className="card" aria-label="Khóa API">
              <div className="row-between">
                <h2>Khóa API</h2>
                <button
                  type="button"
                  className="btn btn-line"
                  onClick={() => {
                    setRekey(true);
                  }}
                >
                  {a.hasCredentials ? "Thay khóa" : "Nhập khóa"}
                </button>
              </div>
              <p>{a.hasCredentials ? "Đã lưu khóa" : "Chưa có khóa: tài khoản ở trạng thái Nháp"}</p>
              <p className="hint">
                Khóa không bao giờ hiện lại. "Thay khóa" mở hộp nhập khóa mới (ô trống), lưu xong tự kiểm tra kết nối.
              </p>
              {a.needsRewrap && (
                <div className="box box-soft small" style={{ marginTop: 8 }}>
                  <b>Khóa đang mã hóa bằng khóa cũ của hệ thống.</b> Nhập lại khóa (Thay khóa) để mã hóa bằng khóa mới;
                  tài khoản vẫn gửi đơn bình thường.
                </div>
              )}
            </section>
          )}
          {a.webhookUrl !== undefined && a.webhookUrl !== "" && perms.can(PERM_MANAGE) && (
            <WebhookCard url={a.webhookUrl} carrier={carrierName(a.carrierKey)} />
          )}
          {api && <LocationsPanel account={a} manage={manage} canReadUnits={perms.can("customer.read")} />}
        </div>
        <div className="stack">
          {api && manage && a.hasCredentials && <CheckCard a={a} checking={checking} />}
          {api && caps !== null && (
            <section className="card" aria-label="Hãng này làm được">
              <h2>Hãng này làm được</h2>
              <ul className="small caps">
                {caps.map((l) => (
                  <li key={l}>
                    {l.includes(": ") ? (
                      <>
                        {l.slice(0, l.indexOf(": ") + 2)}
                        <b>{l.slice(l.indexOf(": ") + 2)}</b>
                      </>
                    ) : (
                      l
                    )}
                  </li>
                ))}
              </ul>
              <p className="hint">Nút trên đơn ẩn/hiện theo danh sách này; máy chủ vẫn kiểm.</p>
            </section>
          )}
          {manage && <StatusCard a={a} />}
          {perms.can("fulfillment.settings.manage") && (
            <PickupPanel warehouseId={a.warehouseId} warehouseName={warehouse || "kho"} />
          )}
        </div>
      </div>
      {rekey && (
        <CredentialsDialog
          account={a}
          onClose={() => {
            setRekey(false);
          }}
        />
      )}
    </>
  );
}

function WebhookCard({ url, carrier }: { url: string; carrier: string }) {
  const [copied, setCopied] = useState<"ok" | "fail" | null>(null);
  return (
    <section className="card" aria-label="Webhook của hãng">
      <h2>URL nhận tin của hãng (webhook)</h2>
      <div className="copy" style={{ marginTop: 8 }}>
        <input className="input" readOnly value={url} aria-label="URL webhook" />
        <button
          type="button"
          className="btn btn-line"
          onClick={() => {
            navigator.clipboard.writeText(url).then(
              () => {
                setCopied("ok");
              },
              () => {
                setCopied("fail");
              },
            );
          }}
        >
          Sao chép
        </button>
      </div>
      {copied !== null && (
        <span className="small" role="status">
          {copied === "ok" ? "Đã sao chép." : "Trình duyệt không cho sao chép; chọn ô rồi sao chép tay."}
        </span>
      )}
      <p className="hint">
        Dán URL này vào trang quản lý của {carrier}. Tin của hãng chỉ là tín hiệu: CRM luôn hỏi lại hãng trước khi đổi
        trạng thái. Chỉ người có quyền quản lý kết nối thấy URL.
      </p>
    </section>
  );
}

export function CarrierAccountDetail() {
  const { id = "" } = useParams();
  const accounts = useAccounts();
  const perms = usePerms();
  const warehouses = useWarehouses();
  const a = accounts.data?.find((x) => x.id === id);
  // Chưa biết quyền hay tên kho thì chưa vẽ (không vẽ bảng chỉ đọc rồi mới hiện ô sửa).
  const ready = perms.loaded && !warehouses.isPending;
  return (
    <div className="carriers">
      <div className="page-h">
        <div className="grow">
          <p className="crumb">
            <Link to="/settings/carriers">Kết nối vận chuyển</Link> / Tài khoản
          </p>
          <h1 className="wrap-any">{a?.name ?? "Tài khoản hãng"}</h1>
          {a !== undefined && (
            <p className="sub">{`${carrierName(a.carrierKey)} · ${a.mode === "api" ? "API" : "Nhập mã tay"}`}</p>
          )}
        </div>
      </div>
      {accounts.isError && (
        <div className="box box-err" role="alert">
          {errorMessage(accounts.error)}
        </div>
      )}
      {(accounts.isPending || (!ready && !accounts.isError)) && <p className="muted">Đang tải…</p>}
      {accounts.isSuccess && ready && a === undefined && (
        <p className="muted">Không có tài khoản hãng này, hoặc bạn không được xem.</p>
      )}
      {a !== undefined && ready && <Detail a={a} />}
    </div>
  );
}
