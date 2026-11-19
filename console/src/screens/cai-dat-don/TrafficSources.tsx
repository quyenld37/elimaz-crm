// Nguồn đơn (mockup docs/mockups/m2/cai-dat-don.html trạng thái C): thêm, đổi tên tại chỗ, đưa lên/xuống bằng nút, tắt,
// bật lại. Thứ tự là thứ tự trong ô chọn khi lên đơn. Nguồn hệ thống (có key, ví dụ website) không có nút Tắt theo
// mockup; nguồn đã tắt đứng cuối, chỉ có Bật lại. Đổi vị trí: mỗi nguồn đổi chỗ là một PATCH riêng có version (API
// không có lệnh sắp xếp cả danh sách); lỗi giữa chừng thì dừng, báo và đọc lại danh sách.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  createTrafficSource,
  updateTrafficSource,
  type CreateTrafficSourceRequest,
  type TrafficSource,
  type UpdateTrafficSourceRequest,
} from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { ORDER_TOPIC, useSources } from "../don/orderData";
import { RefusalBox } from "../staff/staffData";
import "../khach/khach.css";

const MAX_NAME = 80;

interface Patch {
  id: string;
  body: UpdateTrafficSourceRequest;
}

/** Vị trí mới cho các nguồn đang bật theo thứ tự order: chỉ nguồn có vị trí khác chỉ số của nó. */
export function positionPatches(order: readonly TrafficSource[]): Patch[] {
  return order.flatMap((s, i) => (s.position === i ? [] : [{ id: s.id, body: { version: s.version, position: i } }]));
}

export function TrafficSources() {
  const qc = useQueryClient();
  const { items } = useSources();
  const [newName, setNewName] = useState("");
  const [editing, setEditing] = useState<{ id: string; name: string } | null>(null);
  const [nameError, setNameError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const create = useWriteAction((key, body: CreateTrafficSourceRequest) =>
    unwrap(createTrafficSource({ body, headers: { "Idempotency-Key": key } })),
  );
  const update = useWriteAction((key, p: Patch) =>
    unwrap(updateTrafficSource({ path: { sourceId: p.id }, body: p.body, headers: { "Idempotency-Key": key } })),
  );
  const reload = () => {
    void qc.invalidateQueries({ queryKey: queryKey(ORDER_TOPIC, "traffic-sources") });
  };
  const active = items.filter((s) => s.active);
  const off = items.filter((s) => !s.active);
  const pending = busy || create.pending || update.pending;

  /** Chạy lần lượt các PATCH; dừng ở lỗi đầu tiên. Luôn đọc lại danh sách. */
  const runPatches = async (patches: Patch[]): Promise<boolean> => {
    setBusy(true);
    try {
      for (const p of patches) {
        const r = await update.run(p);
        if (!r.ok) return false;
      }
      return true;
    } finally {
      setBusy(false);
      reload();
    }
  };

  const move = (i: number, by: -1 | 1) => {
    const order = [...active];
    const a = order[i];
    const b = order[i + by];
    if (a === undefined || b === undefined) return;
    order[i] = b;
    order[i + by] = a;
    void runPatches(positionPatches(order));
  };

  const takenText = (name: string) => `Đã có nguồn tên "${name}".`;
  const isTaken = (e: unknown) => e instanceof ApiError && e.code === "already_exists";

  const add = async () => {
    const name = newName.trim();
    if (name === "") {
      setNameError("Điền tên nguồn");
      return;
    }
    setNameError(null);
    const r = await create.run({ name });
    if (!r.ok) {
      if (isTaken(r.error)) setNameError(takenText(name));
      return;
    }
    setNewName("");
    reload();
  };

  const rename = async () => {
    if (editing === null) return;
    const s = items.find((x) => x.id === editing.id);
    const name = editing.name.trim();
    if (s === undefined) return;
    if (name === "") {
      setNameError("Điền tên nguồn");
      return;
    }
    setNameError(null);
    setBusy(true);
    const r = await update.run({ id: s.id, body: { version: s.version, name } });
    setBusy(false);
    if (!r.ok) {
      if (isTaken(r.error)) setNameError(takenText(name));
      return;
    }
    setEditing(null);
    reload();
  };

  const err = create.error ?? update.error;
  const shownErr = err !== null && !isTaken(err);

  return (
    <div className="card" style={{ marginTop: 24 }}>
      <div className="row-between">
        <h2>Nguồn đơn</h2>
        <span className="small muted">Thứ tự này là thứ tự trong ô chọn khi lên đơn</span>
      </div>
      <ul className="olist sources">
        {active.map((s, i) =>
          editing?.id === s.id ? (
            <li className="oitem" key={s.id}>
              <span className="grow">
                <input
                  className="input"
                  aria-label="Tên nguồn"
                  maxLength={MAX_NAME}
                  value={editing.name}
                  onChange={(e) => {
                    setEditing({ id: s.id, name: e.target.value });
                  }}
                />
              </span>
              <button
                type="button"
                className="btn btn-dark"
                disabled={pending}
                onClick={() => {
                  void rename();
                }}
              >
                Lưu
              </button>
              <button
                type="button"
                className="btn btn-quiet"
                onClick={() => {
                  setEditing(null);
                  setNameError(null);
                }}
              >
                Hủy
              </button>
            </li>
          ) : (
            <li className="oitem" key={s.id}>
              <span className="grow">
                <b>{s.name}</b>
                {s.key !== undefined && (
                  <>
                    <br />
                    <span className="xs muted">Nguồn hệ thống</span>
                  </>
                )}
              </span>
              <button
                type="button"
                className="ibtn"
                aria-label={`Đưa ${s.name} lên`}
                disabled={pending || i === 0}
                onClick={() => {
                  move(i, -1);
                }}
              >
                ↑
              </button>
              <button
                type="button"
                className="ibtn"
                aria-label={`Đưa ${s.name} xuống`}
                disabled={pending || i === active.length - 1}
                onClick={() => {
                  move(i, 1);
                }}
              >
                ↓
              </button>
              <button
                type="button"
                className="btn btn-quiet"
                disabled={pending}
                onClick={() => {
                  setEditing({ id: s.id, name: s.name });
                  setNameError(null);
                }}
              >
                Đổi tên
              </button>
              {s.key === undefined && (
                <button
                  type="button"
                  className="btn btn-quiet"
                  disabled={pending}
                  onClick={() => {
                    void runPatches([{ id: s.id, body: { version: s.version, active: false } }]);
                  }}
                >
                  Tắt
                </button>
              )}
            </li>
          ),
        )}
        {off.map((s) => (
          <li className="oitem is-off" key={s.id}>
            <span className="grow">
              <b>{s.name}</b> · đã tắt
              <br />
              <span className="xs">Đơn cũ giữ nguồn này; không chọn được cho đơn mới</span>
            </span>
            <button
              type="button"
              className="btn btn-quiet"
              disabled={pending}
              onClick={() => {
                void runPatches([{ id: s.id, body: { version: s.version, active: true } }]);
              }}
            >
              Bật lại
            </button>
          </li>
        ))}
      </ul>
      <div className="inline-f" style={{ marginTop: 12 }}>
        <input
          className="input"
          placeholder="Tên nguồn mới, ví dụ TikTok"
          aria-label="Tên nguồn mới"
          maxLength={MAX_NAME}
          value={newName}
          onChange={(e) => {
            setNewName(e.target.value);
          }}
        />
        <button
          type="button"
          className="btn btn-line"
          disabled={pending}
          onClick={() => {
            void add();
          }}
        >
          Thêm nguồn
        </button>
      </div>
      {nameError !== null && (
        <p className="field-err" style={{ marginTop: 8 }}>
          {nameError}
        </p>
      )}
      {shownErr && (
        <div style={{ marginTop: 12 }}>
          <RefusalBox
            error={err}
            message={create.error !== null ? create.message : update.message}
            conflictTitle="Nguồn đơn vừa được người khác sửa, tải lại"
            onReload={reload}
          />
        </div>
      )}
    </div>
  );
}
