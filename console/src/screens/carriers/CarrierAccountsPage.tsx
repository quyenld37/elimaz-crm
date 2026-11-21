// Màn Kết nối vận chuyển /settings/carriers (kế hoạch M3 Task 132, M3-41; mockup docs/mockups/m3/ket-noi-van-chuyen.html
// trạng thái A, B, D, E; HV03–HV05). Bảng tài khoản hãng (sáu trạng thái), lọc theo tên, hãng, kho; thêm tài khoản là
// hộp thoại; sửa là trang riêng /settings/carriers/:id. Nút sửa, thêm, thay khóa chỉ cho quyền
// fulfillment.carrier_accounts.manage (máy chủ vẫn kiểm). Cột "Hiện trên web" là nguồn duy nhất của danh sách hãng trên
// website (Q-30).
import { useState } from "react";
import { Link } from "react-router";
import type { CarrierAccount } from "../../api";
import { errorMessage } from "../../app/errors";
import { usePerms } from "../don/orderData";
import { AddAccountDialog } from "./CarrierAccountForm";
import { CredentialsDialog } from "./CredentialsDialog";
import { useAccounts, usePendingChecks, useWarehouses } from "./carrierData";
import { CARRIER_CHOICES, ENV_TEXT, accountKind, isChecking, lastCheckCell, statusChip, webText } from "./carrierRules";
import "./carriers.css";

export const PERM_MANAGE = "fulfillment.carrier_accounts.manage";

function RowAction({
  a,
  manage,
  onReplaceKey,
}: {
  a: CarrierAccount;
  manage: boolean;
  onReplaceKey: (a: CarrierAccount) => void;
}) {
  const to = `/settings/carriers/${a.id}`;
  if (!manage || a.status === "revoked") {
    return (
      <Link className="btn btn-quiet" to={to}>
        Xem
      </Link>
    );
  }
  if (a.status === "auth_failed" && a.mode === "api") {
    return (
      <button
        type="button"
        className="btn btn-line"
        onClick={() => {
          onReplaceKey(a);
        }}
      >
        Thay khóa
      </button>
    );
  }
  return (
    <Link className="btn btn-quiet" to={to}>
      {a.mode === "api" && !a.hasCredentials ? "Nhập khóa" : "Sửa"}
    </Link>
  );
}

function AccountRow({
  a,
  warehouse,
  checking,
  manage,
  onReplaceKey,
}: {
  a: CarrierAccount;
  warehouse: string;
  checking: boolean;
  manage: boolean;
  onReplaceKey: (a: CarrierAccount) => void;
}) {
  const chip = checking ? { text: "Đang kiểm tra…", cls: "chip" } : statusChip(a);
  const last = lastCheckCell(a, checking);
  const cls = a.status === "auth_failed" ? "is-err" : a.status === "revoked" ? "is-muted" : undefined;
  return (
    <tr className={cls}>
      <td className="lead">
        <div className="ten">
          <b className="clamp2 wrap-any">{a.name}</b>
          <span className="small muted">
            {accountKind(a)}
            {a.mode === "api" && (
              <>
                {" · "}
                <span className={a.environment === "production" ? "chip chip-line" : "chip"}>
                  {ENV_TEXT[a.environment]}
                </span>
              </>
            )}
          </span>
        </div>
      </td>
      <td data-label="Kho · mặc định">
        {a.isDefaultForWarehouse ? (
          <span>
            {warehouse} · <b>Mặc định</b>
          </span>
        ) : (
          warehouse
        )}
      </td>
      <td data-label="Trạng thái">
        <span className={chip.cls}>{chip.text}</span>
      </td>
      <td data-label="Lần kiểm gần nhất">
        <span className={["small", last.muted ? "muted" : "num", last.err ? "err-text" : ""].join(" ").trim()}>
          {last.text}
        </span>
      </td>
      <td data-label="Hiện trên web">{webText(a)}</td>
      <td className="c-act">
        <RowAction a={a} manage={manage} onReplaceKey={onReplaceKey} />
      </td>
    </tr>
  );
}

export function CarrierAccountsPage() {
  const perms = usePerms();
  const manage = perms.can(PERM_MANAGE);
  const accounts = useAccounts();
  const warehouses = useWarehouses();
  const pending = usePendingChecks();
  const [q, setQ] = useState("");
  const [carrier, setCarrier] = useState("");
  const [wh, setWh] = useState("");
  const [adding, setAdding] = useState(false);
  const [rekey, setRekey] = useState<CarrierAccount | null>(null);

  // Chưa biết quyền hay tên kho thì chưa vẽ bảng (không hiện "Xem" rồi mới đổi thành "Sửa").
  const ready = accounts.isSuccess && perms.loaded && !warehouses.isPending;
  const all = accounts.data ?? [];
  const term = q.trim().toLowerCase();
  const rows = all.filter(
    (a) =>
      (carrier === "" || a.carrierKey === carrier) &&
      (wh === "" || a.warehouseId === wh) &&
      (term === "" || a.name.toLowerCase().includes(term)),
  );
  const addBtn = manage && (
    <button
      type="button"
      className="btn btn-dark"
      disabled={!warehouses.isSuccess}
      onClick={() => {
        setAdding(true);
      }}
    >
      + Thêm tài khoản
    </button>
  );

  return (
    <div className="carriers">
      <div className="page-h">
        <div className="grow">
          <p className="crumb">Vận chuyển / Kết nối vận chuyển</p>
          <h1>Kết nối vận chuyển</h1>
          <p className="sub">
            Tài khoản của shop ở từng hãng. Danh sách hãng hiện trên website lấy từ cột "Hiện trên web" ở đây (một nơi
            lưu).
          </p>
        </div>
        <div className="row">
          {perms.can("fulfillment.settings.manage") && perms.can("fulfillment.read") && (
            <Link className="btn btn-quiet" to="/settings/fulfillment">
              Cài đặt vận chuyển
            </Link>
          )}
          {addBtn}
        </div>
      </div>

      {accounts.isError && (
        <div className="box box-err" role="alert">
          {errorMessage(accounts.error)}
        </div>
      )}
      {!ready && !accounts.isError && <p className="muted">Đang tải…</p>}
      {ready && all.length === 0 && (
        <div className="empty">
          <h2>Chưa kết nối hãng nào</h2>
          <p className="small muted">
            Thêm tài khoản Viettel Post, Best Express, SPX, hoặc tài khoản nhập mã tay để gửi đơn.
          </p>
          {addBtn}
        </div>
      )}
      {ready && all.length > 0 && (
        <>
          <div className="toolbar">
            <input
              className="input is-search"
              placeholder="Tìm theo tên tài khoản"
              aria-label="Tìm tài khoản"
              value={q}
              onChange={(e) => {
                setQ(e.target.value);
              }}
            />
            <div className="select-wrap">
              <select
                aria-label="Hãng"
                value={carrier}
                onChange={(e) => {
                  setCarrier(e.target.value);
                }}
              >
                <option value="">Mọi hãng</option>
                {CARRIER_CHOICES.map((c) => (
                  <option key={c.key} value={c.key}>
                    {c.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="select-wrap">
              <select
                aria-label="Kho"
                value={wh}
                onChange={(e) => {
                  setWh(e.target.value);
                }}
              >
                <option value="">Mọi kho</option>
                {warehouses.items.map((w) => (
                  <option key={w.id} value={w.id}>
                    {w.name}
                  </option>
                ))}
              </select>
            </div>
          </div>
          <div className="tbl-frame">
            <table className="tbl acc" aria-label="Tài khoản hãng">
              <thead>
                <tr>
                  <th>Tài khoản</th>
                  <th>Kho · mặc định</th>
                  <th>Trạng thái</th>
                  <th>Lần kiểm gần nhất</th>
                  <th>Hiện trên web</th>
                  <th className="c-act">
                    <span className="sr-only">Thao tác</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {rows.map((a) => (
                  <AccountRow
                    key={a.id}
                    a={a}
                    warehouse={warehouses.nameOf(a.warehouseId)}
                    checking={isChecking(a, pending[a.id])}
                    manage={manage}
                    onReplaceKey={setRekey}
                  />
                ))}
                {rows.length === 0 && (
                  <tr>
                    <td className="full muted" colSpan={6}>
                      Không có tài khoản khớp bộ lọc.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          <p className="hint" style={{ marginTop: 8 }}>
            Sáu trạng thái: Nháp · Đang kiểm tra · Đang hoạt động · Cần cấp quyền lại · Tạm ngừng · Đã thu hồi. Chỉ tài
            khoản "Đang hoạt động" được báo phí và gửi đơn mới. Tài khoản lỗi không bao giờ tự được thay bằng tài khoản
            khác.
          </p>
        </>
      )}
      {adding && (
        <AddAccountDialog
          warehouses={warehouses.items}
          onClose={() => {
            setAdding(false);
          }}
        />
      )}
      {rekey !== null && (
        <CredentialsDialog
          account={rekey}
          onClose={() => {
            setRekey(null);
          }}
        />
      )}
    </div>
  );
}
