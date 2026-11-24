// Màn Bộ phối (kế hoạch M5 Task 460, M5-48; mockup docs/mockups/m5/web-looks.html, HV19–HV21). Danh sách ≤ 500 bộ sửa
// trên màn rồi lưu cả danh sách theo revision (saveLooks); xem trước hiện/ẩn 350 ms sau khi ngừng sửa (previewLooks).
// - Đang lưu: mọi ô, nút (cả đặt/kéo chấm, chọn ảnh, bỏ thay đổi) khóa.
// - 409 version_conflict: hộp "Giữ bản đang sửa / Tải bản mới" (HV19), không bao giờ ghi đè âm thầm.
// - Báo hiệu realtime content.looks tải lại danh sách; chỉ nạp vào màn khi chưa sửa gì.
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import {
  getLooks,
  saveLooks,
  type Look,
  type LookList as LookListData,
  type LookStatus,
  type SaveLooksRequest,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { Dialog } from "../staff/staffData";
import { moveBanner } from "../web-banners/bannerRules";
import { ConflictDialog, SaveBar, useReload } from "../web-banners/listEditor";
import { LookForm } from "./LookForm";
import { LookList } from "./LookList";
import { LookPreview } from "./LookPreview";
import {
  defaultColor,
  fetchProduct,
  headers,
  looksKey,
  photoOpts,
  productKey,
  useLookPreview,
  useLooks,
  useProductDetails,
  usePerms,
} from "./lookData";
import {
  MAX_LOOKS,
  addItem,
  blockReason,
  cloneLook,
  linkText,
  lookLink,
  newLook,
  removeItem,
  sameLooks,
  toRequest,
} from "./lookRules";
import "./web-looks.css";

const TEXT_READ_ONLY = 'Bạn chỉ có quyền xem nội dung website (thiếu quyền "Quản lý nội dung website").';
const isConflict = (e: unknown) => e instanceof ApiError && e.code === "version_conflict";

export function LookEditor() {
  const q = useLooks();
  return (
    <div className="wlk">
      {q.data === undefined && <Header add={null} />}
      {q.isPending && <p className="muted">Đang tải danh sách bộ phối…</p>}
      {q.isError && <div className="box box-err">{errorMessage(q.error)}</div>}
      {q.data !== undefined && <Editor server={q.data} />}
    </div>
  );
}

function Header({ add }: { add: { disabled: boolean; onClick: () => void } | null }) {
  return (
    <div className="page-h">
      <div className="grow">
        <p className="crumb">Website / Bộ phối</p>
        <h1>Bộ phối</h1>
        <p className="sub">
          Mục "Hôm nay mặc gì?" trên trang chủ: mỗi bộ là một ảnh người mẫu và 1–3 sản phẩm, chấm trên ảnh dẫn tới sản
          phẩm. Bộ nào cũng có liên kết riêng để gửi khách; tối đa 6 bộ hiện trên trang chủ. Giá, size, còn hàng lấy từ
          CRM.
        </p>
      </div>
      <div className="row">
        <button type="button" className="btn btn-dark" disabled={add?.disabled ?? true} onClick={add?.onClick}>
          + Thêm bộ phối
        </button>
      </div>
    </div>
  );
}

function Editor({ server }: { server: LookListData }) {
  const qc = useQueryClient();
  const perms = usePerms();
  const canManage = perms.can("content.manage");
  const canSearch = perms.can("catalog.read");
  const save = useWriteAction((key: string, body: SaveLooksRequest) =>
    unwrap(saveLooks({ body, headers: headers(key) })),
  );

  const [base, setBase] = useState(server);
  const [draft, setDraft] = useState<Look[]>(server.looks);
  const [selected, setSelected] = useState<string | null>(server.looks[0]?.id ?? null);
  const [query, setQuery] = useState("");
  const [conflict, setConflict] = useState(false);
  const [saved, setSaved] = useState(false);
  const [removing, setRemoving] = useState<Look | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const [names, setNames] = useState<ReadonlyMap<string, string>>(new Map());

  // Số lần đặt lại bản đang sửa (lưu xong, Bỏ thay đổi, Tải bản mới): món về sau khi bản đã đặt lại thì bỏ.
  const resets = useRef(0);
  const resetTo = (l: LookListData) => {
    resets.current += 1;
    setBase(l);
    setDraft(l.looks);
  };
  const { reloading, reload } = useReload(
    looksKey,
    () => unwrap(getLooks()),
    (fresh) => {
      resetTo(fresh);
      save.reset();
      setConflict(false);
    },
  );

  const dirty = !sameLooks(draft, base.looks);
  const saving = save.pending;
  const locked = !canManage || saving || reloading;
  // Món vừa chọn được thêm sau khi đọc xong chi tiết mẫu: lúc đó có thể đã bấm Lưu, nên kiểm khóa theo lần vẽ mới nhất.
  const lockedNow = useRef(locked);
  useEffect(() => {
    lockedNow.current = locked;
  });

  // Bản mới nhất đã đọc (báo hiệu content.looks): nạp ngay khi không có thay đổi chưa lưu; đang sửa thì giữ gốc cũ
  // (lưu báo 409).
  if (!dirty && !saving && base !== server) {
    setBase(server);
    setDraft(server.looks);
  }

  const selId = draft.some((l) => l.id === selected) ? selected : (draft[0]?.id ?? null);
  const idx = draft.findIndex((l) => l.id === selId);
  const current = idx >= 0 ? draft[idx] : undefined;

  const preview = useLookPreview(draft, dirty);
  const details = useProductDetails(current?.items.map((i) => i.productId) ?? [], canSearch);
  const baseStatuses = new Map(base.statuses.map((s): [string, LookStatus] => [s.id, s]));
  const statuses: ReadonlyMap<string, LookStatus> = dirty
    ? new Map((preview.data?.statuses ?? []).map((s): [string, LookStatus] => [s.id, s]))
    : baseStatuses;
  const allNames = new Map([
    ...base.products.map((p): [string, string] => [p.id, p.name]),
    ...(preview.data?.products ?? []).map((p): [string, string] => [p.id, p.name]),
    ...[...details.values()].map((p): [string, string] => [p.id, p.name]),
    ...names,
  ]);
  const photos = photoOpts([base.photos, server.photos, preview.data?.photos ?? []], details.values());
  const thumbs = new Map(
    photos.flatMap((p): [string, string][] => (p.thumb === undefined ? [] : [[p.fileId, p.thumb]])),
  );
  const savedIds = new Set(base.looks.map((l) => l.id));
  // Liên kết theo bản đã lưu (tên chưa lưu chưa có trên website; website tìm bộ theo khóa và chuyển tên cũ sang mới).
  const pathOf = (id: string) => baseStatuses.get(id)?.path;
  const blocked = blockReason(draft);
  const full = draft.length >= MAX_LOOKS;

  const edit = (f: (xs: Look[]) => Look[]) => {
    if (locked) return;
    setDraft(f);
    setSaved(false);
    save.reset();
  };
  const update = (l: Look) => {
    edit((xs) => xs.map((x) => (x.id === l.id ? l : x)));
  };
  // Bỏ thay đổi: về bản mới nhất đã đọc (báo hiệu có thể đã mang bản người khác vừa lưu), không về gốc cũ.
  const discard = () => {
    resetTo(server);
    save.reset();
  };

  const add = () => {
    if (full) return;
    const l = newLook(crypto.randomUUID());
    edit((xs) => [...xs, l]);
    setSelected(l.id);
    setQuery("");
  };

  const pickProduct = async (lookId: string, id: string, name: string) => {
    setNames((m) => new Map([...m, [id, name]]));
    const at = resets.current;
    let color = "";
    let extra = photos;
    try {
      const p = await qc.query({ queryKey: productKey(id), queryFn: () => fetchProduct(id) });
      color = defaultColor(p);
      extra = photoOpts([], [p]);
    } catch {
      // Không đọc được chi tiết mẫu: thêm món không màu (website tự chọn), chọn ảnh tay khi ảnh tải được.
    }
    if (lockedNow.current || resets.current !== at) return;
    edit((xs) => xs.map((x) => (x.id === lookId ? addItem(x, id, color, extra) : x)));
  };

  const onSave = async () => {
    if (locked || blocked !== null) return;
    setConflict(false);
    const r = await save.run({ revision: base.revision, looks: toRequest(draft) });
    if (r.ok) {
      qc.setQueryData(looksKey, r.data);
      resetTo(r.data);
      setSaved(true);
    } else if (isConflict(r.error)) setConflict(true);
  };

  const copy = (id: string) => {
    const path = pathOf(id);
    if (path === undefined) return;
    navigator.clipboard.writeText(lookLink(path)).then(
      () => {
        setToast(`Đã chép liên kết bộ phối: ${linkText(path)}`);
      },
      () => {
        setToast(`Trình duyệt không cho chép; liên kết: ${linkText(path)}`);
      },
    );
  };

  const removePath = removing === null ? undefined : pathOf(removing.id);
  const saveError = !conflict && save.message !== null ? save.message : null;

  return (
    <>
      <Header add={{ disabled: locked || full, onClick: add }} />
      {perms.loaded && !canManage && <p className="reason">{TEXT_READ_ONLY}</p>}
      {full && <p className="reason">Đã đủ 500 bộ phối. Xóa một bộ để thêm bộ mới.</p>}
      {draft.length === 0 ? (
        <div className="empty">
          <h2>Chưa có bộ phối nào</h2>
          <p className="muted">Trang chủ đang hiện hàng tròn danh mục. Bấm Thêm bộ phối, ví dụ "Đi làm ngày thường".</p>
        </div>
      ) : (
        <div className="ed">
          <LookList
            looks={draft}
            statuses={statuses}
            names={allNames}
            thumbs={thumbs}
            selected={selId}
            query={query}
            locked={locked}
            saved={savedIds}
            onQuery={setQuery}
            onSelect={setSelected}
            onHome={(id) => {
              edit((xs) => xs.map((x) => (x.id === id ? { ...x, onHome: !x.onHome } : x)));
            }}
            onMove={(i, dir) => {
              edit((xs) => moveBanner(xs, i, dir));
            }}
            onCopy={copy}
          />
          {current !== undefined && (
            <div className="stack">
              <LookForm
                key={current.id}
                look={current}
                n={idx + 1}
                names={allNames}
                photos={photos}
                details={details}
                categories={base.categories}
                autoTitle={statuses.get(current.id)?.autoTitle}
                disabled={locked}
                full={full}
                canSearch={canSearch}
                onChange={update}
                onPickProduct={(id, name) => {
                  void pickProduct(current.id, id, name);
                }}
                onRemoveProduct={(id) => {
                  update(removeItem(current, id, photos));
                }}
                onClone={() => {
                  if (full) return;
                  const c = cloneLook(current, crypto.randomUUID());
                  edit((xs) => {
                    const at = xs.findIndex((x) => x.id === current.id);
                    return [...xs.slice(0, at + 1), c, ...xs.slice(at + 1)];
                  });
                  setSelected(c.id);
                }}
                onRemove={() => {
                  setRemoving(current);
                }}
              />
              <LookPreview
                look={current}
                n={idx + 1}
                status={statuses.get(current.id)}
                names={allNames}
                photo={photos.find((p) => p.fileId === current.photoFileId)?.large}
                error={preview.error === null ? null : errorMessage(preview.error)}
              />
            </div>
          )}
        </div>
      )}
      {toast !== null && (
        <div className="toast" role="status">
          <span className="grow">{toast}</span>
        </div>
      )}

      <SaveBar
        visible={dirty || saving}
        error={saveError}
        done={saved && !dirty ? "Đã lưu bộ phối." : null}
        saving={saving}
        locked={locked}
        blocked={blocked}
        saveLabel="Lưu bộ phối"
        onDiscard={discard}
        onSave={() => {
          void onSave();
        }}
      />

      {removing !== null && (
        <Dialog
          title={`Xóa bộ "${removing.name.trim() === "" ? "Bộ mới" : removing.name}"?`}
          onClose={() => {
            setRemoving(null);
          }}
          footer={
            <>
              <button
                type="button"
                className="btn btn-quiet"
                onClick={() => {
                  setRemoving(null);
                }}
              >
                Hủy
              </button>
              <button
                type="button"
                className="btn btn-danger"
                disabled={locked}
                onClick={() => {
                  const id = removing.id;
                  edit((xs) => xs.filter((x) => x.id !== id));
                  setRemoving(null);
                }}
              >
                Xóa bộ phối
              </button>
            </>
          }
        >
          {removePath === undefined ? (
            <p>Bộ này chưa lưu nên chưa có liên kết. Bấm Lưu bộ phối để áp dụng.</p>
          ) : (
            <p>
              Liên kết đã gửi khách (<span className="small">{linkText(removePath)}</span>) <b>sẽ không mở được nữa</b>.
              Bấm Lưu bộ phối để áp dụng.
            </p>
          )}
        </Dialog>
      )}

      {conflict && (
        <ConflictDialog
          title="Bộ phối vừa được người khác lưu"
          subject="danh sách bộ phối"
          reloading={reloading}
          onKeep={() => {
            setConflict(false);
          }}
          onReload={() => {
            void reload();
          }}
        />
      )}
    </>
  );
}
