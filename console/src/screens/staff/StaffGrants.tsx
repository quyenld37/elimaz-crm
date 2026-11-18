// Màn Nhân viên và quyền — sửa quyền của một người (kế hoạch M1 Task 54, PQ07; mockup docs/mockups/m1/nhan-vien.html,
// trạng thái C, D). Gửi toàn bộ danh sách vai trò · phạm vi kèm version đọc lúc mở (replaceStaffGrants). Vai trò mà
// người sửa chắc chắn không gán được hiện mờ kèm lý do; máy chủ vẫn là nơi quyết (grant_exceeds_actor, last_owner…).
import { useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { replaceStaffGrants, type Grant, type Role, type StaffGrant, type StaffMember } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { RefusalBox, STAFF_KEY, usePermissionKinds, useStaffData } from "./staffData";
import {
  canGrant,
  canGrantSomewhere,
  grantScope,
  roleHint,
  roleName,
  sameGrant,
  scopeFromKey,
  scopeKey,
  scopeLabel,
  scopeOptions,
  warningText,
  warningsFor,
  type PermissionKinds,
  type ScopeRef,
} from "./staffRules";
import "./staff.css";

export const TEXT_SELF_EDIT = "Bạn không thể sửa quyền hay khóa chính mình.";
export const TEXT_CANNOT_GRANT =
  "Bạn không thể cấp quyền mà chính bạn không có (vai trò này có quyền mà tài khoản của bạn không có).";

function Crumb() {
  return (
    <p className="crumb">
      <Link to="/nhan-vien">Nhân viên</Link> / Sửa quyền
    </p>
  );
}

export function StaffGrants() {
  const { id = "" } = useParams();
  const { staff, roles, me } = useStaffData();
  const kinds = usePermissionKinds(me.data?.grants ?? []);
  if (staff.isPending || roles.isPending || me.isPending) return <p className="muted">Đang tải…</p>;
  if (staff.isError || roles.isError || me.isError) {
    return <RefusalBox error={staff.error ?? roles.error ?? me.error} message={null} />;
  }
  const member = staff.data.find((m) => m.id === id);
  if (member === undefined) {
    return (
      <div className="stack">
        <Crumb />
        <p>Không tìm thấy nhân viên này, có thể đã bị xóa.</p>
      </div>
    );
  }
  if (member.id === me.data.userId) {
    return (
      <div className="stack">
        <Crumb />
        <div className="box box-err" role="alert">
          <b>{TEXT_SELF_EDIT}</b> <span>Nhờ một Chủ khác làm việc này.</span>
        </div>
      </div>
    );
  }
  const actor = staff.data.find((m) => m.id === me.data.userId);
  return (
    <GrantsEditor
      key={member.id}
      member={member}
      staff={staff.data}
      roles={roles.data}
      actorGrants={me.data.grants}
      kinds={kinds}
      actorLine={actor === undefined ? null : actorLine(actor, roles.data)}
      reload={async () => (await staff.refetch()).data?.find((m) => m.id === member.id)}
    />
  );
}

function actorLine(actor: StaffMember, roles: readonly Role[]): string {
  const names = [...new Set(actor.grants.map((g) => roleName(g.roleKey, roles)))];
  return names.length === 0 ? actor.displayName : `${actor.displayName} · ${names.join(", ")}`;
}

interface EditorProps {
  member: StaffMember;
  staff: readonly StaffMember[];
  roles: readonly Role[];
  actorGrants: readonly Grant[];
  /** Loại phạm vi của quyền (listPermissions); rỗng là chưa biết (luật chặt). */
  kinds: PermissionKinds;
  actorLine: string | null;
  reload: () => Promise<StaffMember | undefined>;
}

function GrantsEditor({ member, staff, roles, actorGrants, kinds, actorLine, reload }: EditorProps) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const pickName = useId();
  const pickScopeId = useId();
  const [base, setBase] = useState<StaffMember>(member);
  const [draft, setDraft] = useState<StaffGrant[]>(member.grants);
  const [pick, setPick] = useState<string | null>(null);
  const [pickScope, setPickScope] = useState<string>("");
  const save = useWriteAction((key, b: { id: string; version: number; grants: StaffGrant[] }) =>
    unwrap(
      replaceStaffGrants({
        path: { id: b.id },
        body: { version: b.version, grants: b.grants },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );

  const scopes = scopeOptions(staff, draft.map(grantScope));
  const active = roles.filter((r) => r.status === "active");
  const picked = active.find((r) => r.key === pick);
  const pickScopes = picked === undefined ? [] : scopes.filter((s) => canGrant(picked, s, actorGrants, kinds));
  const firstScope = pickScopes.at(0);
  const chosenScope = pickScope !== "" ? pickScope : firstScope === undefined ? "" : scopeKey(firstScope);

  const update = (i: number, g: StaffGrant) => {
    setDraft((d) => d.map((x, j) => (j === i ? g : x)));
  };
  const add = () => {
    if (picked === undefined || chosenScope === "") return;
    const s = scopeFromKey(chosenScope);
    const g: StaffGrant = { roleKey: picked.key, scopeType: s.type, scopeId: s.id };
    setDraft((d) => (d.some((x) => sameGrant(x, g)) ? d : [...d, g]));
    setPick(null);
    setPickScope("");
  };
  const doReload = async () => {
    const fresh = await reload();
    save.reset();
    if (fresh === undefined) return;
    setBase(fresh);
    setDraft(fresh.grants);
  };
  const doSave = async () => {
    const r = await save.run({ id: base.id, version: base.version, grants: draft });
    if (!r.ok) return;
    await qc.invalidateQueries({ queryKey: STAFF_KEY });
    void navigate("/nhan-vien");
  };

  return (
    <div className="stack">
      <Crumb />
      <div className="card">
        <div className="row-between">
          <h2>Quyền của {base.displayName}</h2>
          {actorLine !== null && <span className="small muted">Người sửa: {actorLine}</span>}
        </div>
        <div>
          {draft.length === 0 && <p className="small muted">Chưa có vai trò nào. Thêm vai trò ở dưới.</p>}
          {draft.map((g, i) => (
            <GrantRow
              key={`${String(i)}|${g.roleKey}|${g.scopeType}|${g.scopeId}`}
              g={g}
              roles={roles}
              active={active}
              scopes={scopes}
              actorGrants={actorGrants}
              kinds={kinds}
              warnings={base.grants.some((x) => sameGrant(x, g)) ? warningsFor(g, base.warnings) : []}
              onChange={(next) => {
                update(i, next);
              }}
              onRemove={() => {
                setDraft((d) => d.filter((_, j) => j !== i));
              }}
            />
          ))}
        </div>

        <h3 style={{ marginTop: 16 }}>Thêm vai trò</h3>
        <div className="stack-sm" style={{ marginTop: 8 }} role="radiogroup" aria-label="Vai trò sẽ thêm">
          {active.map((r) => {
            const off = !canGrantSomewhere(r, scopes, actorGrants, kinds);
            const on = pick === r.key;
            return (
              <label key={r.key} className={off ? "role-opt is-off" : on ? "role-opt is-on" : "role-opt"}>
                <input
                  type="radio"
                  name={pickName}
                  disabled={off}
                  checked={on}
                  onChange={() => {
                    setPick(r.key);
                    setPickScope("");
                  }}
                />
                <b>{roleName(r.key, roles)}</b>
                {off ? <span className="why">{TEXT_CANNOT_GRANT}</span> : <span className="d">{roleHint(r)}</span>}
              </label>
            );
          })}
        </div>
        {picked !== undefined && (
          <div className="grant-row" style={{ borderTop: 0 }}>
            <div className="field">
              <label htmlFor={pickScopeId}>Phạm vi của vai trò {roleName(picked.key, roles)}</label>
              <span className="select-wrap">
                <select
                  id={pickScopeId}
                  value={chosenScope}
                  onChange={(e) => {
                    setPickScope(e.target.value);
                  }}
                >
                  {pickScopes.map((s) => (
                    <option key={scopeKey(s)} value={scopeKey(s)}>
                      {scopeLabel(s)}
                    </option>
                  ))}
                </select>
              </span>
            </div>
            <span />
            <button type="button" className="btn btn-line" onClick={add}>
              Thêm
            </button>
          </div>
        )}

        {save.error !== null && (
          <div style={{ marginTop: 16 }}>
            <RefusalBox error={save.error} message={save.message} onReload={() => void doReload()} />
          </div>
        )}
        <div className="savebar is-sticky" style={{ marginTop: 16 }}>
          <Link to="/nhan-vien" className="btn btn-line">
            Hủy
          </Link>
          <button type="button" className="btn btn-dark" disabled={save.pending} onClick={() => void doSave()}>
            Lưu quyền
          </button>
        </div>
      </div>
    </div>
  );
}

interface GrantRowProps {
  g: StaffGrant;
  roles: readonly Role[];
  active: readonly Role[];
  scopes: readonly ScopeRef[];
  actorGrants: readonly Grant[];
  kinds: PermissionKinds;
  warnings: ReturnType<typeof warningsFor>;
  onChange: (g: StaffGrant) => void;
  onRemove: () => void;
}

function GrantRow({ g, roles, active, scopes, actorGrants, kinds, warnings, onChange, onRemove }: GrantRowProps) {
  const roleId = useId();
  const scopeId = useId();
  const scope = grantScope(g);
  // Vai trò đang chọn luôn có trong danh sách (kể cả đã lưu trữ hay đã xóa) để không đổi ngầm khi mở trang.
  const roleOpts = active.some((r) => r.key === g.roleKey)
    ? active
    : [...active, ...roles.filter((r) => r.key === g.roleKey)];
  const name = roleName(g.roleKey, roles);
  return (
    <div className="grant-row-wrap">
      <div className="grant-row">
        <div className="field">
          <label htmlFor={roleId}>Vai trò</label>
          <span className="select-wrap">
            <select
              id={roleId}
              value={g.roleKey}
              onChange={(e) => {
                onChange({ ...g, roleKey: e.target.value });
              }}
            >
              {!roleOpts.some((r) => r.key === g.roleKey) && <option value={g.roleKey}>{name}</option>}
              {roleOpts.map((r) => (
                <option
                  key={r.key}
                  value={r.key}
                  disabled={r.key !== g.roleKey && !canGrant(r, scope, actorGrants, kinds)}
                >
                  {roleName(r.key, roles)}
                </option>
              ))}
            </select>
          </span>
        </div>
        <div className="field">
          <label htmlFor={scopeId}>Phạm vi</label>
          <span className="select-wrap">
            <select
              id={scopeId}
              value={scopeKey(scope)}
              onChange={(e) => {
                const s = scopeFromKey(e.target.value);
                onChange({ ...g, scopeType: s.type, scopeId: s.id });
              }}
            >
              {scopes.map((s) => {
                const r = roles.find((x) => x.key === g.roleKey);
                const off = scopeKey(s) !== scopeKey(scope) && r !== undefined && !canGrant(r, s, actorGrants, kinds);
                return (
                  <option key={scopeKey(s)} value={scopeKey(s)} disabled={off}>
                    {scopeLabel(s)}
                  </option>
                );
              })}
            </select>
          </span>
        </div>
        <button type="button" className="ibtn" aria-label={`Gỡ vai trò ${name}`} onClick={onRemove}>
          ×
        </button>
      </div>
      {warnings.map((w) => (
        <p key={`${w.code}|${w.scopeType}|${w.scopeId}`} className="small err-text wrap-any grant-warn">
          {warningText(w, roles)}
        </p>
      ))}
    </div>
  );
}
