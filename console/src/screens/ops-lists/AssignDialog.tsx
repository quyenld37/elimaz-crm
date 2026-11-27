// Hộp "Giao việc cho người khác" (mockup docs/mockups/m3/van-hanh.html trạng thái B). Danh sách nhân viên đọc bằng
// listStaff và listRoles (đòi identity.users.manage nên nút chỉ hiện cho người có quyền đó, QĐ-135-a); chỉ hiện nhân
// viên đang hoạt động có vai trò mang quyền gọi lại khách. Gửi `version` của việc đang xem.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { assignCallbackTask, type CallbackTask } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../shared";
import { DialogActions, DialogNotes } from "../shipping/dialogParts";
import { Dialog, useStaffData } from "../staff/staffData";
import { headers, reloadOps } from "./opsData";

export const CALLBACK_PERMISSION = "fulfillment.callbacks.work";

export function AssignDialog({ task, onClose }: { task: CallbackTask; onClose: () => void }) {
  const qc = useQueryClient();
  const { staff, roles } = useStaffData();
  const [to, setTo] = useState("");
  const write = useWriteAction((key, assigneeId: string) =>
    unwrap(
      assignCallbackTask({ path: { id: task.id }, body: { assigneeId, version: task.version }, headers: headers(key) }),
    ),
  );
  const allowed = new Set(
    (roles.data ?? [])
      .filter((r) => r.status === "active" && r.permissions.includes(CALLBACK_PERMISSION))
      .map((r) => r.key),
  );
  const people = (staff.data ?? []).filter(
    (s) => s.status === "active" && s.grants.some((g) => allowed.has(g.roleKey)),
  );
  const loading = staff.isPending || roles.isPending;
  const why = to === "" ? "Chọn người nhận việc" : null;
  const submit = async () => {
    const r = await write.run(to);
    reloadOps(qc);
    if (r.ok) onClose();
  };
  return (
    <Dialog
      title="Giao việc cho người khác"
      closeButton
      onClose={onClose}
      footer={
        <DialogActions
          label="Giao"
          why={why}
          pending={write.pending}
          onSubmit={() => void submit()}
          onClose={onClose}
        />
      }
    >
      {loading && <p className="small muted">Đang tải nhân viên…</p>}
      {!loading && (staff.isError || roles.isError) && (
        <div className="box box-err small" role="alert">
          Chưa tải được danh sách nhân viên.
        </div>
      )}
      {!loading && !staff.isError && !roles.isError && (
        <label className="field">
          <span>Người nhận việc *</span>
          <select
            className="input"
            value={to}
            onChange={(e) => {
              setTo(e.target.value);
            }}
          >
            <option value="">Chọn nhân viên</option>
            {people.map((s) => (
              <option key={s.id} value={s.id}>
                {s.displayName}
              </option>
            ))}
          </select>
        </label>
      )}
      <p className="small muted">Chỉ hiện nhân viên có quyền gọi lại khách.</p>
      <DialogNotes why={why} message={write.message} />
    </Dialog>
  );
}
