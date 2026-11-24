// Màn Banner trang chủ (kế hoạch M5 Task 459, M5-47; mockup docs/mockups/m5/web-banners.html, HV18, HV19). Danh sách
// ≤ 8 banner sửa trên màn rồi lưu cả danh sách theo revision (saveBanners); xem trước hiện/ẩn 350 ms sau khi ngừng sửa
// (previewBanners); ảnh banner tải lên kho tệp mục đích web_banner (useUploads).
// - Đang lưu: mọi ô, nút (cả kéo thả, tải ảnh, bỏ thay đổi) khóa bằng một fieldset.
// - 409 version_conflict: hộp "Giữ bản đang sửa / Tải bản mới", không bao giờ ghi đè âm thầm; giữ bản thì revision cũ
//   vẫn được gửi (lần lưu sau lại báo 409).
// - Báo hiệu realtime content.banners tải lại danh sách; chỉ nạp vào màn khi chưa sửa gì.
import { useQueryClient } from "@tanstack/react-query";
import { useState, type DragEvent } from "react";
import {
  getBanners,
  saveBanners,
  type HomeBanner,
  type HomeBannerKind,
  type HomeBannerList,
  type HomeBannerStatus,
  type SaveBannersRequest,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { moveTo, rejectText } from "../products/imageRules";
import { useUploads, type UploadItem, type UploadOptions } from "../products/useUpload";
import { BannerForm } from "./BannerForm";
import { ConflictDialog, SaveBar, useReload } from "./listEditor";
import { fileProblem } from "./BannerImage";
import { BannerPreview, savedImageUrl } from "./BannerPreview";
import { bannersKey, headers, useBannerFileWatch, useBannerPreview, useBanners, usePerms } from "./bannerData";
import {
  KINDS,
  KIND_TEXT,
  MAX_BANNERS,
  blockReason,
  changeKind,
  cloneBanner,
  hiddenLine,
  moveBanner,
  newBanner,
  sameBanners,
  toRequest,
  type ImageState,
  type Slot,
} from "./bannerRules";
import "./web-banners.css";

const TEXT_READ_ONLY = 'Bạn chỉ có quyền xem nội dung website (thiếu quyền "Quản lý nội dung website").';
const isKind = (v: string): v is HomeBannerKind => KINDS.some((k) => k === v);
const isConflict = (e: unknown) => e instanceof ApiError && e.code === "version_conflict";

/** Tên hiện của banner ở danh sách: tiêu đề, mô tả ảnh, tên chiến dịch, danh mục, hoặc loại. */
function bannerName(b: HomeBanner, l: HomeBannerList): string {
  if (b.title !== undefined && b.title !== "") return b.title;
  if (b.kind === "image" && b.alt !== undefined && b.alt !== "") return b.alt;
  if (b.kind === "campaign") return l.campaigns.find((c) => c.id === b.campaignId)?.name ?? KIND_TEXT[b.kind];
  if (b.kind === "category") return l.categories.find((c) => c.id === b.categoryId)?.name ?? KIND_TEXT[b.kind];
  return KIND_TEXT[b.kind];
}

const slotKey = (id: string, slot: Slot) => `${id}:${slot}`;

function uploadState(it: UploadItem): ImageState {
  switch (it.phase) {
    case "uploading":
      return { phase: "uploading" };
    case "processing":
      return { phase: "processing" };
    case "failed":
      return { phase: "rejected", ...(it.message === undefined ? {} : { message: it.message }) };
    default:
      return { phase: "ready" };
  }
}

export function BannerEditor({ uploadOptions }: { uploadOptions?: UploadOptions }) {
  const q = useBanners();
  return (
    <div className="wbn">
      <div className="page-h">
        <div className="grow">
          <p className="crumb">Website / Banner trang chủ</p>
          <h1>Banner trang chủ</h1>
          <p className="sub">
            Tối đa 8 banner chạy lần lượt trên trang chủ. Banner tự động lấy sản phẩm và giá từ CRM; banner ảnh hiện
            đúng ảnh thiết kế. Lưu là lưu cả danh sách.
          </p>
        </div>
      </div>
      {q.isPending && <p className="muted">Đang tải danh sách banner…</p>}
      {q.isError && <div className="box box-err">{errorMessage(q.error)}</div>}
      {q.data !== undefined && <Editor server={q.data} uploadOptions={uploadOptions ?? {}} />}
    </div>
  );
}

function Editor({ server, uploadOptions }: { server: HomeBannerList; uploadOptions: UploadOptions }) {
  const qc = useQueryClient();
  const perms = usePerms();
  const canManage = perms.can("content.manage");
  const canUpload = perms.can("files.upload");
  const canSearch = perms.can("catalog.read");
  const save = useWriteAction((key: string, body: SaveBannersRequest) =>
    unwrap(saveBanners({ body, headers: headers(key) })),
  );
  const uploads = useUploads({ ...uploadOptions, purpose: "web_banner" });

  const [base, setBase] = useState(server);
  const [draft, setDraft] = useState<HomeBanner[]>(server.banners);
  const [selected, setSelected] = useState<string | null>(server.banners[0]?.id ?? null);
  const [newKind, setNewKind] = useState<HomeBannerKind>("campaign");
  const [conflict, setConflict] = useState(false);
  const [saved, setSaved] = useState(false);
  const [names, setNames] = useState<ReadonlyMap<string, string>>(new Map());
  const [slots, setSlots] = useState<Readonly<Record<string, { key: string; name: string }>>>({});
  const [problems, setProblems] = useState<Readonly<Record<string, string>>>({});
  const [dragFrom, setDragFrom] = useState<number | null>(null);

  const resetTo = (l: HomeBannerList) => {
    setBase(l);
    setDraft(l.banners);
    setSlots({});
    setProblems({});
  };
  const { reloading, reload } = useReload(
    bannersKey,
    () => unwrap(getBanners()),
    (fresh) => {
      resetTo(fresh);
      save.reset();
      setConflict(false);
    },
  );

  const dirty = !sameBanners(draft, base.banners);
  const saving = save.pending;
  const locked = !canManage || saving || reloading;

  // Bản mới nhất đã đọc (báo hiệu content.banners): nạp ngay khi không có thay đổi chưa lưu — kể cả khi người dùng vừa
  // hoàn tác tay về đúng gốc cũ — để màn không vẽ bản cũ như bản đã lưu. Đang sửa thì giữ gốc cũ (lưu báo 409).
  if (!dirty && !saving && base !== server) {
    setBase(server);
    setDraft(server.banners);
  }

  // Ảnh vừa có mã tệp: gắn vào ô của banner (một lần; bỏ ảnh xóa ô khỏi slots).
  for (const [k, s] of Object.entries(slots)) {
    const it = uploads.items.find((x) => x.key === s.key);
    const [id, slot] = k.split(":");
    const field = slot === "mobile" ? "imageMobile" : "imageDesktop";
    const b = draft.find((x) => x.id === id);
    if (it?.fileId !== undefined && b !== undefined && b[field] !== it.fileId) {
      const fileId = it.fileId;
      setDraft((xs) => xs.map((x) => (x.id === id ? { ...x, [field]: fileId } : x)));
    }
  }

  const preview = useBannerPreview(draft, dirty);
  const statuses: ReadonlyMap<string, HomeBannerStatus> = new Map(
    (preview.statuses ?? (dirty ? [] : base.statuses)).map((s) => [s.id, s]),
  );
  const previewError = preview.error;
  const allNames = new Map([...base.products.map((p): [string, string] => [p.id, p.name]), ...names]);
  const images = [...server.images, ...base.images];

  const slotState = (b: HomeBanner, slot: Slot): ImageState | null => {
    const s = slots[slotKey(b.id, slot)];
    const it = s === undefined ? undefined : uploads.items.find((x) => x.key === s.key);
    const fileId = slot === "desktop" ? b.imageDesktop : b.imageMobile;
    if (it !== undefined && (it.fileId === undefined || it.fileId === fileId)) return uploadState(it);
    if (fileId === undefined) return null;
    const img = images.find((x) => x.fileId === fileId);
    if (img === undefined || img.deleted || img.status === "lost") return { phase: "missing" };
    if (img.status === "ready") return { phase: "ready" };
    if (img.status === "rejected") return { phase: "rejected", message: rejectText(img.rejectReason) };
    return { phase: "processing" };
  };
  const blocked = blockReason(draft, slotState);
  // Ảnh đã lưu còn đang xử lý: báo hiệu tệp đổi trạng thái thì đọc lại danh sách (Task 488).
  useBannerFileWatch(
    server.images
      .filter((x) => !x.deleted && (x.status === "uploaded" || x.status === "processing"))
      .map((x) => x.fileId),
    canUpload,
  );
  /** Cỡ ảnh gốc máy chủ đã đo (ảnh sẵn sàng của danh sách); ảnh vừa tải chưa có trong danh sách → undefined. */
  const sizeOf = (fileId: string | undefined) => {
    const img = images.find((x) => x.fileId === fileId);
    return img?.width === undefined || img.height === undefined ? undefined : { w: img.width, h: img.height };
  };

  const edit = (next: HomeBanner[]) => {
    if (locked) return;
    setDraft(next);
    setSaved(false);
    save.reset();
  };
  const update = (b: HomeBanner) => {
    edit(draft.map((x) => (x.id === b.id ? b : x)));
  };
  const add = () => {
    if (draft.length >= MAX_BANNERS) return;
    const b = newBanner(newKind, crypto.randomUUID());
    edit([...draft, b]);
    setSelected(b.id);
  };

  const onSave = async () => {
    if (locked || blocked !== null) return;
    setConflict(false);
    const r = await save.run({ revision: base.revision, banners: toRequest(draft) });
    if (r.ok) {
      qc.setQueryData(bannersKey, r.data);
      resetTo(r.data);
      setSaved(true);
    } else if (isConflict(r.error)) {
      setConflict(true);
      // Đọc bản mới (không thay bản đang sửa) để hộp nêu ai lưu, lúc nào.
      void qc.invalidateQueries({ queryKey: bannersKey });
    }
  };

  const pick = (b: HomeBanner, slot: Slot, f: File) => {
    const k = slotKey(b.id, slot);
    const problem = fileProblem(f);
    setProblems((p) => {
      const rest = Object.fromEntries(Object.entries(p).filter(([x]) => x !== k));
      return problem === null ? rest : { ...rest, [k]: problem };
    });
    if (problem !== null || locked || !canUpload) return;
    const [key] = uploads.add([f]);
    if (key !== undefined) setSlots((s) => ({ ...s, [k]: { key, name: f.name } }));
    setSaved(false);
  };
  const unpick = (b: HomeBanner, slot: Slot) => {
    const k = slotKey(b.id, slot);
    setSlots((s) => Object.fromEntries(Object.entries(s).filter(([x]) => x !== k)));
    update(slot === "desktop" ? { ...b, imageDesktop: undefined } : { ...b, imageMobile: undefined });
  };

  const onDrop = (to: number) => (e: DragEvent) => {
    e.preventDefault();
    if (dragFrom !== null && !locked) edit(moveTo(draft, dragFrom, to));
    setDragFrom(null);
  };

  // Banner đang sửa suy ra mỗi lần vẽ: banner đã chọn còn trong danh sách thì giữ, không thì banner đầu (bản mới từ
  // máy chủ, Bỏ thay đổi, Tải bản mới có thể bỏ mất banner đã chọn).
  const selId = draft.some((b) => b.id === selected) ? selected : (draft[0]?.id ?? null);
  const idx = draft.findIndex((b) => b.id === selId);
  const current = idx >= 0 ? draft[idx] : undefined;
  const shownCount = draft.filter((b) => statuses.get(b.id)?.shown === true).length;
  const hiddenCount = draft.filter((b) => statuses.get(b.id)?.shown === false).length;
  const full = draft.length >= MAX_BANNERS;
  const saveError = !conflict && save.message !== null ? save.message : null;

  return (
    <>
      {perms.loaded && !canManage && <p className="reason">{TEXT_READ_ONLY}</p>}
      <div className="ed">
        <section className="card stack bl">
          <div className="row-between">
            <h2>Banner trên trang chủ</h2>
            <span className="small muted">
              {shownCount} đang hiện · {hiddenCount} đang ẩn
            </span>
          </div>
          <ol className="olist" aria-label="Banner trên trang chủ">
            {draft.map((b, i) => {
              const line = hiddenLine(statuses.get(b.id));
              const hidden = statuses.get(b.id)?.shown === false;
              return (
                <li
                  key={b.id}
                  className={`oitem${b.id === selId ? " is-pick" : ""}${saving ? " is-off" : ""}`}
                  draggable={!locked}
                  onDragStart={() => {
                    if (!locked) setDragFrom(i);
                  }}
                  onDragOver={(e) => {
                    e.preventDefault();
                  }}
                  onDrop={onDrop(i)}
                >
                  <span className="ord">{i + 1}</span>
                  <button
                    type="button"
                    className="grow wbn-pick"
                    aria-current={b.id === selId}
                    onClick={() => {
                      setSelected(b.id);
                    }}
                  >
                    <b>{bannerName(b, base)}</b>
                    <br />
                    <span className="small muted">
                      {KIND_TEXT[b.kind]}
                      {line !== "" && !hidden ? ` · ${line}` : " · "}
                    </span>
                    {hidden && <span className="why-hide">{line}</span>}
                  </button>
                  <div className="row">
                    <label className={`sw${locked ? " is-off" : ""}`}>
                      <input
                        type="checkbox"
                        disabled={locked}
                        aria-label={`Bật banner ${String(i + 1)}`}
                        checked={b.enabled}
                        onChange={() => {
                          update({ ...b, enabled: !b.enabled });
                        }}
                      />{" "}
                      Bật
                    </label>
                    <button
                      type="button"
                      className="ibtn"
                      aria-label={`Đưa banner ${String(i + 1)} lên`}
                      disabled={locked || i === 0}
                      onClick={() => {
                        edit(moveBanner(draft, i, -1));
                      }}
                    >
                      ↑
                    </button>
                    <button
                      type="button"
                      className="ibtn"
                      aria-label={`Đưa banner ${String(i + 1)} xuống`}
                      disabled={locked || i === draft.length - 1}
                      onClick={() => {
                        edit(moveBanner(draft, i, 1));
                      }}
                    >
                      ↓
                    </button>
                  </div>
                </li>
              );
            })}
          </ol>
          {saving && <p className="reason">Trong lúc lưu không bật/tắt, đổi thứ tự, sửa hay kéo thả được.</p>}
          <div className="inline-f">
            <span className="select-wrap grow">
              <select
                aria-label="Loại banner mới"
                value={newKind}
                disabled={locked || full}
                onChange={(e) => {
                  if (isKind(e.target.value)) setNewKind(e.target.value);
                }}
              >
                {KINDS.map((k) => (
                  <option key={k} value={k}>
                    {KIND_TEXT[k]}
                  </option>
                ))}
              </select>
            </span>
            <button type="button" className="btn btn-line" disabled={locked || full} onClick={add}>
              + Thêm banner
            </button>
          </div>
          {full && <p className="reason">Đã đủ 8 banner. Xóa một banner để thêm banner mới.</p>}
        </section>

        {current === undefined ? (
          <div className="empty">
            <h2>Chưa có banner</h2>
            <p className="muted">
              Trang chủ đang dùng banner mặc định. Chọn một loại ở "Thêm banner" để bắt đầu, ví dụ Giảm giá cho chiến
              dịch đang chạy.
            </p>
          </div>
        ) : (
          <BannerForm
            key={current.id}
            banner={current}
            n={idx + 1}
            list={base}
            names={allNames}
            disabled={locked}
            full={full}
            canSearch={canSearch}
            onChange={update}
            onKind={(k) => {
              // Ảnh đang tải của banner này không gắn vào loại mới.
              setSlots((x) =>
                Object.fromEntries(Object.entries(x).filter(([key]) => !key.startsWith(`${current.id}:`))),
              );
              update(changeKind(current, k));
            }}
            onPickProduct={(id, name) => {
              setNames((m) => new Map([...m, [id, name]]));
            }}
            onClone={() => {
              if (full) return;
              const c = cloneBanner(current, crypto.randomUUID());
              edit([...draft.slice(0, idx + 1), c, ...draft.slice(idx + 1)]);
              setSelected(c.id);
            }}
            onRemove={() => {
              const next = draft.filter((b) => b.id !== current.id);
              edit(next);
              setSelected(next[Math.min(idx, next.length - 1)]?.id ?? null);
            }}
            image={(slot) => {
              const k = slotKey(current.id, slot);
              const name = slots[k]?.name;
              const fileId = slot === "desktop" ? current.imageDesktop : current.imageMobile;
              const size = sizeOf(fileId);
              return {
                fileId,
                state: slotState(current, slot),
                ...(size === undefined ? {} : { size }),
                ...(name === undefined ? {} : { name }),
                problem: problems[k] ?? null,
                canUpload,
                onPick: (f) => {
                  pick(current, slot, f);
                },
                onRemove: () => {
                  unpick(current, slot);
                },
              };
            }}
          />
        )}
      </div>

      <SaveBar
        visible={dirty || saving}
        error={saveError}
        done={saved && !dirty ? "Đã lưu danh sách banner." : null}
        saving={saving}
        locked={locked}
        blocked={blocked}
        saveLabel="Lưu banner"
        onDiscard={() => {
          // Về bản mới nhất đã đọc (báo hiệu có thể đã mang bản người khác vừa lưu), không về gốc cũ.
          resetTo(server);
          save.reset();
        }}
        onSave={() => {
          void onSave();
        }}
      />

      <BannerPreview
        banners={draft}
        statuses={statuses}
        url={savedImageUrl(images)}
        error={previewError === null ? null : errorMessage(previewError)}
      />

      {conflict && (
        <ConflictDialog
          title="Banner vừa được người khác lưu"
          subject="danh sách banner"
          server={server}
          base={base}
          reloading={reloading}
          onKeep={() => {
            setConflict(false);
          }}
          onReload={reload}
        />
      )}
    </>
  );
}
