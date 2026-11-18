// Màn Nhân viên và quyền — danh sách (kế hoạch M1 Task 54, PQ07; mockup docs/mockups/m1/nhan-vien.html, trạng thái A,
// B, D, E). Dòng của chính mình không có nút sửa quyền, khóa (API vẫn chặn: cannot_modify_self). Cảnh báo
// admin_needs_shop_scope của Task 315 hiện dưới vai trò bị ảnh hưởng.
import { useQueryClient } from "@tanstack/react-query";
import { useId, useState, type SyntheticEvent } from "react";
import { Link } from "react-router";
import { createStaff, lockStaff, unlockStaff, type Role, type StaffMember } from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { Dialog, RefusalBox, STAFF_KEY, useStaffData } from "./staffData";
import { grantLabel, warningText } from "./staffRules";
import "./staff.css";

export const TEXT_SELF_ROW = "Không tự sửa quyền hay khóa chính mình";
export const TEXT_NO_GRANTS = "Chưa có quyền nào";

export function StaffList() {
  const { staff, roles, me } = useStaffData();
  const qc = useQueryClient();
  const [adding, setAdding] = useState(false);
  const [locking, setLocking] = useState<StaffMember | null>(null);
  const setLock = useWriteAction((key, b: { id: string; lock: boolean }) => {
    const opts = { path: { id: b.id }, headers: { "Idempotency-Key": key } };
    return b.lock ? unwrap(lockStaff(opts)) : unwrap(unlockStaff(opts));
  });

  const runLock = async (m: StaffMember, lock: boolean) => {
    const r = await setLock.run({ id: m.id, lock });
    setLocking(null);
    if (r.ok) await qc.invalidateQueries({ queryKey: STAFF_KEY });
  };

  const roleList = roles.data ?? [];
  return (
    <>
      <div className="page-h">
        <div>
          <h1>Nhân viên và quyền</h1>
          <p className="sub">
            Mỗi người có một hay nhiều vai trò, mỗi vai trò gắn một phạm vi (toàn shop hoặc một kho). Không ai sửa được
            quyền của chính mình.
          </p>
        </div>
        <button
          type="button"
          className="btn btn-dark"
          onClick={() => {
            setAdding(true);
          }}
        >
          Thêm nhân viên
        </button>
      </div>

      {setLock.error !== null && (
        <div className="stack" style={{ marginBottom: 16 }}>
          <RefusalBox error={setLock.error} message={setLock.message} />
        </div>
      )}

      {staff.isPending && <p className="muted">Đang tải…</p>}
      {staff.isError && <RefusalBox error={staff.error} message={null} />}
      {staff.data !== undefined && (
        <div className="tbl-frame">
          <table className="tbl ntbl">
            <thead>
              <tr>
                <th>Nhân viên</th>
                <th>Vai trò · phạm vi</th>
                <th>Trạng thái</th>
                <th className="c-act">
                  <span className="sr">Thao tác</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {staff.data.map((m) => (
                <StaffRow
                  key={m.id}
                  m={m}
                  roles={roleList}
                  self={m.id === me.data?.userId}
                  busy={setLock.pending}
                  onLock={() => {
                    setLock.reset();
                    setLocking(m);
                  }}
                  onUnlock={() => void runLock(m, false)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {adding && (
        <AddStaffDialog
          onDone={() => {
            setAdding(false);
          }}
        />
      )}
      {locking !== null && (
        <Dialog
          title={`Khóa ${locking.displayName}?`}
          onClose={() => {
            setLocking(null);
          }}
          footer={
            <>
              <button
                type="button"
                className="btn btn-line"
                onClick={() => {
                  setLocking(null);
                }}
              >
                Quay lại
              </button>
              <button
                type="button"
                className="btn btn-danger"
                disabled={setLock.pending}
                onClick={() => void runLock(locking, true)}
              >
                Khóa
              </button>
            </>
          }
        >
          <p>
            {locking.displayName} không dùng được CRM từ thao tác kế tiếp. Vai trò giữ nguyên; mở khóa là dùng lại được.
          </p>
        </Dialog>
      )}
    </>
  );
}

interface StaffRowProps {
  m: StaffMember;
  roles: readonly Role[];
  self: boolean;
  busy: boolean;
  onLock: () => void;
  onUnlock: () => void;
}

function StaffRow({ m, roles, self, busy, onLock, onUnlock }: StaffRowProps) {
  const locked = m.status === "locked";
  const noGrants = m.grants.length === 0;
  return (
    <tr className={locked ? "is-muted" : undefined}>
      <td className="lead">
        <span className="who">
          <b>{m.displayName}</b>
          {self && (
            <>
              {" "}
              <span className="chip chip-dark">Bạn</span>
            </>
          )}
          <br />
          <span className="small muted wrap-any">{m.email}</span>
        </span>
      </td>
      <td data-label="Vai trò">
        <span className="grant-cell">
          {noGrants ? (
            <span className="chip chip-err">{TEXT_NO_GRANTS}</span>
          ) : (
            <span className="grants">
              {m.grants.map((g) => (
                <span key={`${g.roleKey}|${g.scopeType}|${g.scopeId}`} className="tag wrap-any">
                  {grantLabel(g, roles)}
                </span>
              ))}
            </span>
          )}
          {(m.warnings ?? []).map((w) => (
            <span key={`${w.code}|${w.roleKey ?? ""}|${w.scopeType}|${w.scopeId}`} className="small err-text wrap-any">
              {warningText(w, roles)}
            </span>
          ))}
        </span>
      </td>
      <td data-label="Trạng thái">
        {locked ? (
          <span className="chip chip-line">Đã khóa</span>
        ) : (
          <span className="chip chip-ok">Đang hoạt động</span>
        )}
      </td>
      {self ? (
        <td className="c-act small muted">{TEXT_SELF_ROW}</td>
      ) : (
        <td className="c-act">
          <div className="row">
            <Link to={`/nhan-vien/${m.id}/quyen`} className={noGrants ? "btn btn-dark" : "btn btn-quiet"}>
              {noGrants ? "Gán vai trò" : "Sửa quyền"}
            </Link>
            {locked ? (
              <button type="button" className="btn btn-line" disabled={busy} onClick={onUnlock}>
                Mở khóa
              </button>
            ) : (
              <button type="button" className="btn btn-quiet" disabled={busy} onClick={onLock}>
                Khóa
              </button>
            )}
          </div>
        </td>
      )}
    </tr>
  );
}

function AddStaffDialog({ onDone }: { onDone: () => void }) {
  const qc = useQueryClient();
  const formId = useId();
  const emailId = useId();
  const nameId = useId();
  const emailErrId = useId();
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const create = useWriteAction((key, body: { email: string; displayName: string }) =>
    unwrap(createStaff({ body, headers: { "Idempotency-Key": key } })),
  );
  const emailTaken = create.error instanceof ApiError && create.error.code === "email_taken";

  const submit = async (e: SyntheticEvent) => {
    e.preventDefault();
    const r = await create.run({ email: email.trim(), displayName: name.trim() });
    if (!r.ok) return;
    await qc.invalidateQueries({ queryKey: STAFF_KEY });
    onDone();
  };

  return (
    <Dialog
      title="Thêm nhân viên"
      drawer
      closeButton
      onClose={onDone}
      footer={
        <>
          <button type="button" className="btn btn-line" onClick={onDone}>
            Hủy
          </button>
          <button type="submit" form={formId} className="btn btn-dark" disabled={create.pending}>
            Thêm
          </button>
        </>
      }
    >
      <form id={formId} className="stack" onSubmit={(e) => void submit(e)}>
        <div className="field">
          <label htmlFor={emailId}>Email đăng nhập *</label>
          <input
            id={emailId}
            className={emailTaken ? "input is-err" : "input"}
            type="email"
            required
            maxLength={254}
            autoComplete="off"
            value={email}
            aria-invalid={emailTaken}
            aria-describedby={emailTaken ? emailErrId : undefined}
            onChange={(e) => {
              setEmail(e.target.value);
            }}
          />
          {emailTaken && (
            <span id={emailErrId} className="field-err">
              {errorMessage(create.error)}.
            </span>
          )}
          <span className="hint">Đúng email người đó dùng để đăng nhập CRM (có bước hai).</span>
        </div>
        <div className="field">
          <label htmlFor={nameId}>Tên hiển thị *</label>
          <input
            id={nameId}
            className="input"
            required
            maxLength={200}
            value={name}
            onChange={(e) => {
              setName(e.target.value);
            }}
          />
        </div>
        {create.error !== null && !emailTaken && <RefusalBox error={create.error} message={create.message} />}
        <p className="small muted">Thêm xong thì gán vai trò; trước đó người này chưa dùng được màn nào.</p>
      </form>
    </Dialog>
  );
}
