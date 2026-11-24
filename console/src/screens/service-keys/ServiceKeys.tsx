// Màn Khóa dịch vụ /website/khoa-dich-vu (kế hoạch M5 Task 208, M5-25; mockup docs/mockups/m5/service-keys.html trạng
// thái A–D; HV13; ADR-18). Khóa để website gọi CRM: danh sách chỉ có tiền tố; khóa đầy đủ (trường secret của câu trả lời
// 201) chỉ nằm trong state của hộp "Đã tạo khóa", đóng hộp là mất — không vào localStorage, sessionStorage, bộ đệm
// React Query hay console. Gửi lại cùng khóa chống trùng (mạng rớt) → 409 already_processed: không hiện lại được.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { createServiceKey, listServiceKeys, revokeServiceKey, type ServiceKey } from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { shortTime } from "../../app/time";
import { Dialog } from "../staff/staffData";
import {
  KEY_PERMISSIONS,
  MAX_NAME,
  fullDate,
  keyStatus,
  parseCidrs,
  permChips,
  prefixText,
  validateCreate,
} from "./serviceKeyRules";
import "./service-keys.css";

export const KEYS_KEY = queryKey("identity.service_key", "list");

const isCode = (e: unknown, status: number, code: string) =>
  e instanceof ApiError && e.status === status && e.code === code;

function KeyRow({ k, onRevoke }: { k: ServiceKey; onRevoke: (k: ServiceKey) => void }) {
  const st = keyStatus(k);
  const revoked = k.revokedAt !== undefined;
  return (
    <tr className={revoked ? "is-muted" : undefined}>
      <td className="lead">
        <div className="stack-sm sk-name">
          <b className="clamp2 wrap-any" title={k.name}>
            {k.name}
          </b>
          <span className="small muted">Tạo {fullDate(k.createdAt)}</span>
        </div>
      </td>
      <td data-label="Tiền tố">
        <span className="code">{prefixText(k)}</span>
      </td>
      <td data-label="Quyền">
        <div className="perms">
          {permChips(k.permissions).map((c) => (
            <span key={c} className="chip">
              {c}
            </span>
          ))}
        </div>
      </td>
      <td data-label="IP được gọi">
        <span className="small num">{k.allowedCidrs.length === 0 ? "Mọi địa chỉ" : k.allowedCidrs.join(", ")}</span>
      </td>
      <td data-label="Lần dùng cuối">
        {k.lastUsedAt === undefined ? (
          <span className="small muted">Chưa dùng</span>
        ) : (
          <span className="small num">{shortTime(k.lastUsedAt).replace(" ", " · ")}</span>
        )}
      </td>
      <td data-label="Trạng thái">
        <span className={st.cls}>{st.text}</span>
      </td>
      <td className="c-act">
        {revoked ? (
          <span className="small muted">—</span>
        ) : (
          <button
            type="button"
            className="btn btn-danger"
            onClick={() => {
              onRevoke(k);
            }}
          >
            Thu hồi
          </button>
        )}
      </td>
    </tr>
  );
}

function CreateKeyDialog({
  onClose,
  onCreated,
  onReplayed,
}: {
  onClose: () => void;
  onCreated: (name: string, secret: string) => void;
  onReplayed: (name: string) => void;
}) {
  const [name, setName] = useState("");
  const [perms, setPerms] = useState<string[]>(KEY_PERMISSIONS.filter((p) => p.byDefault).map((p) => p.key));
  const [ips, setIps] = useState("");
  const [touched, setTouched] = useState(false);
  const create = useWriteAction((key: string, body: { name: string; permissions: string[]; allowedCidrs: string[] }) =>
    unwrap(createServiceKey({ body, headers: { "Idempotency-Key": key } })),
  );
  const errs = validateCreate({ name, perms, ips });
  const bad = Object.keys(errs).length > 0;
  const show = (field: keyof typeof errs) => (touched || field !== "name" ? errs[field] : undefined);

  const submit = async () => {
    const parsed = parseCidrs(ips);
    if (bad || "error" in parsed) return;
    // Giữ đúng thứ tự quyền của danh sách (thân giống nhau thì dùng lại khóa chống trùng khi bấm lại).
    const permissions = KEY_PERMISSIONS.map((p) => p.key).filter((k) => perms.includes(k));
    const r = await create.run({ name: name.trim(), permissions, allowedCidrs: parsed.cidrs });
    if (r.ok) onCreated(r.data.name, r.data.secret);
    else if (isCode(r.error, 409, "already_processed")) onReplayed(name.trim());
  };

  const replayed = isCode(create.error, 409, "already_processed");
  return (
    <Dialog
      title="Tạo khóa dịch vụ"
      closeButton
      className="sk-dlg"
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn btn-quiet" onClick={onClose}>
            Đóng
          </button>
          <button
            type="button"
            className="btn btn-dark"
            disabled={bad || create.pending}
            onClick={() => {
              void submit();
            }}
          >
            {create.pending ? "Đang tạo…" : "Tạo khóa"}
          </button>
        </>
      }
    >
      <label className="field">
        <span>Tên khóa *</span>
        <input
          className="input"
          aria-label="Tên khóa"
          value={name}
          maxLength={MAX_NAME + 20}
          aria-invalid={show("name") !== undefined}
          onChange={(e) => {
            setName(e.target.value);
            setTouched(true);
          }}
        />
        {show("name") === undefined ? (
          <span className="hint">1–80 ký tự; ghi khóa dùng ở đâu để sau này biết thu hồi khóa nào.</span>
        ) : (
          <span className="hint err-text">{show("name")}</span>
        )}
      </label>
      <fieldset className="perm-d">
        <legend className="label">Quyền của khóa *</legend>
        {KEY_PERMISSIONS.map((p) => (
          <label key={p.key} className="perm">
            <input
              type="checkbox"
              checked={perms.includes(p.key)}
              onChange={(e) => {
                const on = e.target.checked;
                setPerms((cur) => (on ? [...cur, p.key] : cur.filter((x) => x !== p.key)));
              }}
            />
            <span>
              <b>{p.label}</b>
              {p.hint !== undefined && (
                <>
                  <br />
                  <span className="small muted">{p.hint}</span>
                </>
              )}
            </span>
          </label>
        ))}
        {show("perms") !== undefined && <p className="hint err-text">{show("perms")}</p>}
      </fieldset>
      <label className="field">
        <span>IP được gọi</span>
        <textarea
          className="input"
          aria-label="IP được gọi"
          value={ips}
          placeholder="Để trống: mọi địa chỉ. Mỗi dòng một địa chỉ hoặc dải, ví dụ 13.140.64.56/32"
          aria-invalid={show("ips") !== undefined}
          onChange={(e) => {
            setIps(e.target.value);
          }}
        />
        {show("ips") === undefined ? (
          <span className="hint">Nên giới hạn đúng địa chỉ máy chủ website.</span>
        ) : (
          <span className="hint err-text">{show("ips")}</span>
        )}
      </label>
      {create.message !== null && !replayed && (
        <div className="box box-err" role="alert">
          {create.message}
        </div>
      )}
    </Dialog>
  );
}

function SecretDialog({ name, secret, onClose }: { name: string; secret: string; onClose: () => void }) {
  const [copied, setCopied] = useState<"ok" | "fail" | null>(null);
  const copy = () => {
    // clipboard thiếu (trang không an toàn) → lỗi trong then, báo "chưa chép được".
    Promise.resolve()
      .then(() => navigator.clipboard.writeText(secret))
      .then(
        () => {
          setCopied("ok");
        },
        () => {
          setCopied("fail");
        },
      );
  };
  return (
    <Dialog
      title={`Đã tạo khóa "${name}"`}
      className="sk-dlg"
      onClose={onClose}
      footer={
        <button type="button" className="btn btn-dark" onClick={onClose}>
          Tôi đã chép, đóng
        </button>
      }
    >
      <div className="once">
        <p>
          <b className="err-text">Khóa chỉ hiện một lần này.</b> Chép ngay và dán vào cấu hình của website (biến{" "}
          <span className="code">CRM_SERVICE_KEY</span>). Đóng hộp này là không xem lại được; CRM chỉ giữ dấu băm của
          khóa.
        </p>
        <div className="copy">
          <input
            className="input"
            readOnly
            autoComplete="off"
            spellCheck={false}
            aria-label="Khóa dịch vụ"
            value={secret}
            onFocus={(e) => {
              e.target.select();
            }}
          />
          <button type="button" className="btn btn-dark" onClick={copy}>
            Chép
          </button>
        </div>
        {copied === "ok" && <p className="small ok-text">Đã chép vào bộ nhớ tạm.</p>}
        {copied === "fail" && (
          <p className="small err-text">Chưa chép được. Bấm vào ô khóa, chọn hết rồi chép bằng tay.</p>
        )}
      </div>
      <p className="hint">
        Màn hình không lưu khóa vào trình duyệt. Nếu lỡ đóng trước khi chép: thu hồi khóa này và tạo khóa mới.
      </p>
    </Dialog>
  );
}

function RevokeDialog({
  k,
  onClose,
  onDone,
  onConflict,
}: {
  k: ServiceKey;
  onClose: () => void;
  onDone: () => void;
  onConflict: () => void;
}) {
  const revoke = useWriteAction((key: string, b: { id: string; version: number }) =>
    unwrap(revokeServiceKey({ path: { id: b.id }, body: { version: b.version }, headers: { "Idempotency-Key": key } })),
  );
  const go = async () => {
    const r = await revoke.run({ id: k.id, version: k.version });
    if (r.ok) onDone();
    else if (isCode(r.error, 409, "version_conflict")) onConflict();
  };
  return (
    <Dialog
      title="Thu hồi khóa này?"
      closeButton
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn btn-quiet" onClick={onClose}>
            Đóng
          </button>
          <button
            type="button"
            className="btn btn-danger"
            disabled={revoke.pending}
            onClick={() => {
              void go();
            }}
          >
            {revoke.pending ? "Đang thu hồi…" : "Thu hồi khóa"}
          </button>
        </>
      }
    >
      <p>
        <b className="wrap-any">{k.name}</b> · <span className="code">{prefixText(k)}</span>
      </p>
      <p>
        Trong vài giây, mọi lần gọi bằng khóa này bị từ chối: website dùng khóa này sẽ không đọc được sản phẩm, không
        gửi được đơn. <b>Không mở lại được</b>; muốn dùng tiếp thì tạo khóa mới.
      </p>
      {revoke.message !== null && (
        <div className="box box-err" role="alert">
          {revoke.message}
        </div>
      )}
    </Dialog>
  );
}

export function ServiceKeys() {
  const qc = useQueryClient();
  const keys = useQuery({ queryKey: KEYS_KEY, queryFn: () => unwrap(listServiceKeys()) });
  const [creating, setCreating] = useState(false);
  // Khóa vừa tạo: chỉ trong state này, đóng hộp là xóa.
  const [created, setCreated] = useState<{ name: string; secret: string } | null>(null);
  const [replayed, setReplayed] = useState<string | null>(null);
  const [revoking, setRevoking] = useState<ServiceKey | null>(null);
  const [conflict, setConflict] = useState(false);
  const reload = () => {
    void qc.invalidateQueries({ queryKey: KEYS_KEY });
  };

  const all = keys.data ?? [];
  const createBtn = (
    <button
      type="button"
      className="btn btn-dark"
      onClick={() => {
        setReplayed(null);
        setCreating(true);
      }}
    >
      + Tạo khóa
    </button>
  );

  return (
    <div className="service-keys">
      <div className="page-h">
        <div className="grow">
          <p className="crumb">Website / Khóa dịch vụ</p>
          <h1>Khóa dịch vụ</h1>
          <p className="sub">
            Khóa để website gọi CRM (đọc sản phẩm, gửi đơn, yêu cầu, góp ý…). Mỗi khóa chỉ có đúng các quyền website
            cần; khóa đầy đủ chỉ hiện một lần lúc tạo.
          </p>
        </div>
        <div className="row">{createBtn}</div>
      </div>

      {replayed !== null && (
        <div className="box box-err stack-sm" role="alert">
          <b>Khóa đã được tạo ở lần bấm trước, không hiện lại được.</b>
          <p>
            Mạng chập chờn nên màn hình gửi lại; CRM không tạo khóa thứ hai. Nếu bạn chưa kịp chép khóa: thu hồi khóa
            &quot;{replayed}&quot; trong danh sách rồi tạo khóa mới.
          </p>
          <div>
            <button
              type="button"
              className="btn btn-line"
              onClick={() => {
                setReplayed(null);
              }}
            >
              Về danh sách
            </button>
          </div>
        </div>
      )}
      {conflict && (
        <div className="box box-err stack-sm" role="alert">
          <b>Khóa vừa được người khác đổi.</b>
          <p>Đã tải lại danh sách. Xem lại trạng thái khóa trong bảng.</p>
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

      {keys.isError && (
        <div className="box box-err" role="alert">
          {errorMessage(keys.error)}
        </div>
      )}
      {keys.isPending && <p className="muted">Đang tải…</p>}
      {keys.isSuccess && all.length === 0 && (
        <div className="empty">
          <h2>Chưa có khóa nào</h2>
          <p className="muted">Website chưa nối được CRM. Tạo một khóa rồi dán vào cấu hình website.</p>
          {createBtn}
        </div>
      )}
      {keys.isSuccess && all.length > 0 && (
        <>
          <div className="tbl-frame">
            <table className="tbl sk" aria-label="Khóa dịch vụ">
              <thead>
                <tr>
                  <th>Tên</th>
                  <th>Tiền tố</th>
                  <th>Quyền</th>
                  <th>IP được gọi</th>
                  <th>Lần dùng cuối</th>
                  <th>Trạng thái</th>
                  <th className="c-act">
                    <span className="sr-only">Thao tác</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {all.map((k) => (
                  <KeyRow
                    key={k.id}
                    k={k}
                    onRevoke={(x) => {
                      setConflict(false);
                      setRevoking(x);
                    }}
                  />
                ))}
              </tbody>
            </table>
          </div>
          <p className="hint sk-note">
            Danh sách không bao giờ có khóa đầy đủ, chỉ có tiền tố để nhận ra khóa nào đang được dùng ở đâu.
          </p>
        </>
      )}

      {creating && (
        <CreateKeyDialog
          onClose={() => {
            setCreating(false);
          }}
          onCreated={(name, secret) => {
            setCreating(false);
            setCreated({ name, secret });
            reload();
          }}
          onReplayed={(name) => {
            setCreating(false);
            setReplayed(name);
            reload();
          }}
        />
      )}
      {created !== null && (
        <SecretDialog
          name={created.name}
          secret={created.secret}
          onClose={() => {
            setCreated(null);
          }}
        />
      )}
      {revoking !== null && (
        <RevokeDialog
          k={revoking}
          onClose={() => {
            setRevoking(null);
          }}
          onDone={() => {
            setRevoking(null);
            reload();
          }}
          onConflict={() => {
            setRevoking(null);
            setConflict(true);
            reload();
          }}
        />
      )}
    </div>
  );
}
