// Màn Vai trò — xem, tạo, sửa, lưu trữ một vai trò (kế hoạch M1 Task M1-56, Q35; mockup docs/mockups/m1/vai-tro.html
// trạng thái B, C, D). Quyền người sửa không có hiện mờ kèm lý do (thêm hay gỡ một quyền cần có quyền đó trên toàn shop);
// máy chủ vẫn là nơi quyết (grant_exceeds_actor, owner_role_locked, role_in_use…).
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type ReactNode } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { archiveRole, createRole, listStaff, updateRole, type Grant, type PermissionInfo, type Role } from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { hasPermission } from "../../app/nav";
import { Dialog, RefusalBox, STAFF_KEY } from "../staff/staffData";
import { ROLES_KEY, useRoleData } from "./roleData";
import {
  OWNER_KEY,
  blockedChanges,
  canChange,
  groupPermissions,
  isReadOnly,
  roleKind,
  scopeChip,
  toSubmit,
} from "./roleRules";
import "../staff/staff.css";
import "./roles.css";

export const NEW_ROLE_ID = "moi";
export const TEXT_CANNOT_CHANGE = "Bạn không có quyền này nên không cấp được";
export const TEXT_ROLE_CONFLICT = "Vai trò vừa được người khác sửa, tải lại";
export const TEXT_OWNER_INFO =
  "Vai trò Chủ luôn có mọi quyền, kể cả quyền mới thêm sau này. Không sửa, không lưu trữ được.";
export const TEXT_ARCHIVED_INFO = "Vai trò đã lưu trữ: không gán được cho ai, không sửa được.";

function Crumb({ label }: { label: string }) {
  return (
    <p className="crumb">
      <Link to="/vai-tro">Vai trò</Link> / {label}
    </p>
  );
}

export function RoleEdit() {
  const { id = "" } = useParams();
  const { roles, permissions, me } = useRoleData();
  if (roles.isPending || permissions.isPending || me.isPending) return <p className="muted">Đang tải…</p>;
  if (roles.isError || permissions.isError || me.isError) {
    return <RefusalBox error={roles.error ?? permissions.error ?? me.error} message={null} />;
  }
  if (id === NEW_ROLE_ID) {
    return <RoleEditor key="new" role={null} catalog={permissions.data} actor={me.data.grants} />;
  }
  const role = roles.data.find((r) => r.id === id);
  if (role === undefined) {
    return (
      <div className="stack">
        <Crumb label="Không tìm thấy" />
        <p>Không tìm thấy vai trò này.</p>
      </div>
    );
  }
  if (isReadOnly(role)) return <RoleView role={role} catalog={permissions.data} />;
  return (
    <RoleEditor
      key={role.id}
      role={role}
      catalog={permissions.data}
      actor={me.data.grants}
      reload={async () => (await roles.refetch()).data?.find((r) => r.id === role.id)}
    />
  );
}

/** Trạng thái B: chỉ xem (vai trò Chủ, vai trò đã lưu trữ). */
function RoleView({ role, catalog }: { role: Role; catalog: readonly PermissionInfo[] }) {
  const owner = role.key === OWNER_KEY;
  const has = new Set(role.permissions);
  return (
    <div className="stack">
      <Crumb label={role.name} />
      <div className="card">
        <div className="row-between">
          <h2 className="wrap-any">{role.name}</h2>
          <span className="chip">Chỉ xem</span>
        </div>
        <div className="box box-info">{owner ? TEXT_OWNER_INFO : TEXT_ARCHIVED_INFO}</div>
        <div className="stack" style={{ marginTop: 16 }}>
          {groupPermissions(catalog).map((grp) => (
            <PermGroup
              key={grp.group}
              title={grp.group}
              count={grp.items.filter((p) => has.has(p.key)).length}
              total={grp.items.length}
            >
              {grp.items.map((p) => (
                <PermItem key={p.key} p={p} checked={has.has(p.key)} disabled />
              ))}
            </PermGroup>
          ))}
        </div>
      </div>
    </div>
  );
}

interface PermGroupProps {
  title: string;
  count: number;
  total: number;
  children: ReactNode;
}

function PermGroup({ title, count, total, children }: PermGroupProps) {
  return (
    <div className="perm-g">
      <div className="row-between">
        <h3>{title}</h3>
        <span className="small muted">
          {count}/{total}
        </span>
      </div>
      <div className="perm-list">{children}</div>
    </div>
  );
}

interface PermItemProps {
  p: PermissionInfo;
  checked: boolean;
  disabled: boolean;
  why?: string;
  onToggle?: (on: boolean) => void;
}

function PermItem({ p, checked, disabled, why, onToggle }: PermItemProps) {
  const chip = scopeChip(p);
  return (
    <label className={disabled ? "perm is-off" : "perm"}>
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => {
          onToggle?.(e.target.checked);
        }}
      />
      <span>
        {p.label}
        {chip !== null && <span className="chip">{chip}</span>}
        <br />
        <code>{p.key}</code>
      </span>
      {why !== undefined && <span className="why">{why}</span>}
    </label>
  );
}

interface EditorProps {
  role: Role | null;
  catalog: readonly PermissionInfo[];
  actor: readonly Grant[];
  reload?: () => Promise<Role | undefined>;
}

/** Trạng thái C (sửa, tạo) và D (lưu trữ, câu báo). */
function RoleEditor({ role, catalog, actor, reload }: EditorProps) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const nameId = useId();
  const [base, setBase] = useState<Role | null>(role);
  const [name, setName] = useState(role?.name ?? "");
  const [selected, setSelected] = useState<Set<string>>(() => new Set(role?.permissions ?? []));
  const [confirmArchive, setConfirmArchive] = useState(false);
  const [blockedArchive, setBlockedArchive] = useState(false);
  const canSeeStaff = hasPermission(actor, "identity.users.manage");
  const staff = useQuery({
    queryKey: STAFF_KEY,
    queryFn: () => unwrap(listStaff()),
    enabled: blockedArchive && canSeeStaff,
  });

  const save = useWriteAction((key, b: { id: string | null; version: number; name: string; permissions: string[] }) =>
    b.id === null
      ? unwrap(createRole({ body: { name: b.name, permissions: b.permissions }, headers: { "Idempotency-Key": key } }))
      : unwrap(
          updateRole({
            path: { id: b.id },
            body: { version: b.version, name: b.name, permissions: b.permissions },
            headers: { "Idempotency-Key": key },
          }),
        ),
  );
  const archive = useWriteAction((key, b: { id: string; version: number }) =>
    unwrap(archiveRole({ path: { id: b.id }, body: { version: b.version }, headers: { "Idempotency-Key": key } })),
  );

  const blocked = blockedChanges(base?.permissions ?? [], selected, catalog, actor);
  const toggle = (key: string, on: boolean) => {
    setSelected((s) => {
      const next = new Set(s);
      if (on) next.add(key);
      else next.delete(key);
      return next;
    });
  };
  const done = async () => {
    await qc.invalidateQueries({ queryKey: ROLES_KEY });
    void navigate("/vai-tro");
  };
  const doSave = async () => {
    archive.reset();
    const r = await save.run({
      id: base?.id ?? null,
      version: base?.version ?? 0,
      name: name.trim(),
      permissions: toSubmit(selected, catalog),
    });
    if (r.ok) await done();
  };
  const doReload = async () => {
    const fresh = await reload?.();
    save.reset();
    archive.reset();
    if (fresh === undefined) return;
    setBase(fresh);
    setName(fresh.name);
    setSelected(new Set(fresh.permissions));
  };
  const startArchive = async () => {
    save.reset();
    archive.reset();
    const fresh = await reload?.();
    if (fresh !== undefined && fresh.version !== base?.version) {
      setBase(fresh);
      setName(fresh.name);
      setSelected(new Set(fresh.permissions));
    }
    if ((fresh ?? base)?.holderCount !== 0) {
      setBlockedArchive(true);
      return;
    }
    setBlockedArchive(false);
    setConfirmArchive(true);
  };
  const doArchive = async () => {
    if (base === null) return;
    const r = await archive.run({ id: base.id, version: base.version });
    setConfirmArchive(false);
    if (r.ok) {
      await done();
      return;
    }
    if (r.error instanceof ApiError && r.error.code === "role_in_use") {
      archive.reset();
      setBlockedArchive(true);
      await reload?.();
    }
  };

  const title = base === null ? "Tạo vai trò" : "Sửa vai trò";
  const holders =
    base === null || staff.data === undefined
      ? []
      : staff.data.filter((m) => m.status === "active" && m.grants.some((g) => g.roleKey === base.key));
  const writeError = save.error ?? archive.error;
  return (
    <div className="stack">
      <Crumb label={base === null ? "Tạo vai trò" : base.name} />
      <div className="card">
        <div className="row-between">
          <h2>{title}</h2>
          {base !== null && (
            <span className="small muted">
              {roleKind(base)} · {base.holderCount} người đang giữ
            </span>
          )}
        </div>
        <div className="field">
          <label htmlFor={nameId}>Tên vai trò *</label>
          <input
            id={nameId}
            className="input"
            required
            maxLength={100}
            value={name}
            onChange={(e) => {
              setName(e.target.value);
            }}
          />
        </div>
        <div className="row-between" style={{ marginTop: 16 }}>
          <b>Quyền ({toSubmit(selected, catalog).length} đã chọn)</b>
          <span className="small muted">Thêm hay gỡ một quyền thì bạn phải có quyền đó trên toàn shop.</span>
        </div>
        <div className="stack" style={{ marginTop: 8 }}>
          {groupPermissions(catalog).map((grp) => (
            <PermGroup
              key={grp.group}
              title={grp.group}
              count={grp.items.filter((p) => selected.has(p.key)).length}
              total={grp.items.length}
            >
              {grp.items.map((p) => {
                const off = !canChange(p, actor);
                return (
                  <PermItem
                    key={p.key}
                    p={p}
                    checked={selected.has(p.key)}
                    disabled={off}
                    why={off ? TEXT_CANNOT_CHANGE : undefined}
                    onToggle={(on) => {
                      toggle(p.key, on);
                    }}
                  />
                );
              })}
            </PermGroup>
          ))}
        </div>

        {blockedArchive && base !== null && (
          <div className="box box-err" role="alert" style={{ marginTop: 16 }}>
            <b>
              Không lưu trữ được: còn {base.holderCount} người đang giữ vai trò "{base.name}".
            </b>{" "}
            <span>
              {holders.length > 0 && `${holders.map((m) => m.displayName).join(", ")}. `}Gỡ vai trò này khỏi họ ở màn
              Nhân viên trước.
            </span>
            {canSeeStaff && (
              <div className="row" style={{ marginTop: 8 }}>
                <Link className="btn btn-line" to="/nhan-vien">
                  Mở màn Nhân viên
                </Link>
              </div>
            )}
          </div>
        )}
        {writeError !== null && (
          <div style={{ marginTop: 16 }}>
            <RefusalBox
              error={writeError}
              message={save.error !== null ? save.message : archive.message}
              conflictTitle={TEXT_ROLE_CONFLICT}
              onReload={reload === undefined ? undefined : () => void doReload()}
            />
          </div>
        )}
        {writeError === null && blocked.length > 0 && (
          <p className="small err-text" style={{ marginTop: 16 }}>
            Bạn không thể cấp quyền mà chính bạn không có: {blocked.map((p) => p.label).join(", ")}.
          </p>
        )}
        <div className="savebar is-sticky" style={{ marginTop: 16 }}>
          {base !== null && (
            <button
              type="button"
              className="btn btn-danger"
              disabled={archive.pending || save.pending}
              onClick={() => void startArchive()}
            >
              Lưu trữ vai trò
            </button>
          )}
          <Link to="/vai-tro" className="btn btn-line">
            Hủy
          </Link>
          <button
            type="button"
            className="btn btn-dark"
            disabled={save.pending || name.trim() === ""}
            onClick={() => void doSave()}
          >
            Lưu
          </button>
        </div>
      </div>
      {confirmArchive && base !== null && (
        <Dialog
          title={`Lưu trữ vai trò "${base.name}"?`}
          onClose={() => {
            setConfirmArchive(false);
          }}
          footer={
            <>
              <button
                type="button"
                className="btn btn-line"
                onClick={() => {
                  setConfirmArchive(false);
                }}
              >
                Quay lại
              </button>
              <button
                type="button"
                className="btn btn-dark"
                disabled={archive.pending}
                onClick={() => void doArchive()}
              >
                Lưu trữ
              </button>
            </>
          }
        >
          <p>Vai trò không còn trong danh sách gán ở màn Nhân viên. Không ai đang giữ vai trò này.</p>
        </Dialog>
      )}
    </div>
  );
}
