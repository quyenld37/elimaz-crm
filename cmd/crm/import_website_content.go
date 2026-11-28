package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// bannerFileRe là đường của một ảnh banner trong thư mục xuất (exportBanner của website): không cho đường lạ, "..".
var bannerFileRe = regexp.MustCompile(`^banner-images/[a-f0-9]{32}\.webp$`)

// imagePollEvery là nhịp hỏi trạng thái ảnh banner khi chờ worker xử lý.
const imagePollEvery = 2 * time.Second

// bannerImage là kết quả chuẩn bị một tệp ảnh banner: mã tệp CRM (chạy khô: mã tạm coi như đã xử lý) hay lỗi.
type bannerImage struct {
	ID          uuid.UUID
	Placeholder bool
	Err         error
}

var (
	errBannerFile      = errors.New("tệp ảnh banner sai")
	errImageRejected   = errors.New("kho tệp từ chối ảnh")
	errImageWaitExpire = errors.New("hết giờ chờ ảnh được xử lý")
)

// prepareBannerImages chuẩn bị ảnh của các banner chưa có trong CRM, ngoài mọi transaction nghiệp vụ: đọc và kiểm tệp
// tại máy (đường đúng mẫu, ≤ 8 MiB, đầu tệp WebP/JPEG/PNG); --apply: tải lên theo đường tải lên của kho tệp (CreateUpload
// trong tx ngắn, Put ngoài tx, CompleteUpload xếp file.process) rồi chờ ready; chạy khô: mã tạm. Ảnh lỗi chỉ làm banner
// đó lỗi (ở bước banner). Lỗi trả về là lỗi đọc danh sách banner của CRM.
func (im *websiteImporter) prepareBannerImages(ctx context.Context, dir string, x wxExport, c *importCount) (
	map[string]bannerImage, error,
) {
	out := map[string]bannerImage{}
	if x.Banners == nil {
		return out, nil
	}
	var have map[uuid.UUID]bool
	if err := tenancy.WithTenant(ctx, im.DB, im.Tenant, func(tx pgx.Tx) error {
		set, err := content.GetBanners(ctx, tx)
		have = idSet(set.Banners, func(b content.Banner) uuid.UUID { return b.ID })
		return err //nolint:wrapcheck // content đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc banner của CRM: %w", err)
	}
	n := 0
	for _, rel := range bannerFiles(x.Banners.Items, have) {
		out[rel] = im.bannerImage(ctx, dir, rel)
		if out[rel].Err == nil {
			n++
		}
	}
	if !im.DryRun && n > 0 {
		c.note("đã tải lên và xử lý %d ảnh banner", n)
	}
	return out, nil
}

// idSet trả tập mã của list (id đọc mã của một phần tử).
func idSet[T any](list []T, id func(T) uuid.UUID) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(list))
	for _, v := range list {
		out[id(v)] = true
	}
	return out
}

// bannerFiles trả đường các tệp ảnh (bỏ trùng, theo thứ tự gặp) của các banner chưa có trong CRM (have).
func bannerFiles(items []wxBanner, have map[uuid.UUID]bool) []string {
	var out []string
	for _, b := range items {
		if have[b.ID] || b.Image == nil {
			continue
		}
		for _, p := range []*wxPicture{b.Image.Desktop, b.Image.Mobile} {
			if p != nil && !slices.Contains(out, p.File) {
				out = append(out, p.File)
			}
		}
	}
	return out
}

func (im *websiteImporter) bannerImage(ctx context.Context, dir, rel string) bannerImage {
	if !bannerFileRe.MatchString(rel) {
		return bannerImage{Err: fmt.Errorf("%w: đường %q", errBannerFile, rel)}
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel))) //nolint:gosec // G304: rel đúng bannerFileRe, không ".."
	if err != nil {
		return bannerImage{Err: fmt.Errorf("%w: đọc %s: %w", errBannerFile, rel, err)}
	}
	ct := imageType(data)
	switch {
	case len(data) == 0 || len(data) > content.MaxBannerBytes:
		return bannerImage{Err: fmt.Errorf("%w: %s có %d byte (1–%d)", errBannerFile, rel, len(data), content.MaxBannerBytes)}
	case ct == "":
		return bannerImage{Err: fmt.Errorf("%w: %s không phải ảnh WebP, JPEG, PNG", errBannerFile, rel)}
	case im.DryRun:
		return bannerImage{ID: uuid.New(), Placeholder: true}
	}
	id, err := im.uploadBannerImage(ctx, filepath.Base(rel), ct, data)
	if err != nil {
		return bannerImage{Err: fmt.Errorf("ảnh %s: %w", rel, err)}
	}
	return bannerImage{ID: id}
}

// imageType nhận loại ảnh theo đầu tệp; rỗng khi không phải WebP, JPEG, PNG.
func imageType(b []byte) string {
	switch {
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "image/webp"
	case len(b) >= 3 && bytes.Equal(b[:3], []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	}
	return ""
}

func (im *websiteImporter) uploadBannerImage(ctx context.Context, name, ct string, data []byte) (uuid.UUID, error) {
	id, err := uploadFile(ctx, im.DB, im.Files, im.Queue, im.Tenant, content.BannerPurposeKey, name, ct, data)
	if err != nil {
		return uuid.Nil, err
	}
	if err := im.WaitImage(ctx, id); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// uploadFile tải data lên kho tệp theo đường tải lên (CreateUpload trong tx ngắn, Put ngoài tx, CompleteUpload xếp
// file.process) với mục đích purpose và trả mã tệp; việc chờ xử lý xong (ảnh) là của bên gọi.
func uploadFile(ctx context.Context, d *db.DB, store files.FileStore, q queue.JobQueue, tenant uuid.UUID,
	purpose, name, ct string, data []byte,
) (uuid.UUID, error) {
	var f files.File
	if err := tenancy.WithTenant(ctx, d, tenant, func(tx pgx.Tx) error {
		var err error
		f, _, err = files.CreateUpload(ctx, tx, store, files.UploadInput{
			TenantID: tenant, Purpose: purpose, FileName: name, ContentType: ct, SizeBytes: int64(len(data)),
		}, importSystemID())
		return err //nolint:wrapcheck // files đã bọc
	}); err != nil {
		return uuid.Nil, fmt.Errorf("tạo tệp tải lên: %w", err)
	}
	if err := store.Put(ctx, f.Bucket, f.Key, bytes.NewReader(data), int64(len(data)), ct); err != nil {
		return uuid.Nil, fmt.Errorf("ghi tệp lên kho: %w", err)
	}
	if _, err := files.CompleteUpload(ctx, d, store, q, tenant, f.ID); err != nil {
		return uuid.Nil, fmt.Errorf("hoàn tất tải lên: %w", err)
	}
	return f.ID, nil
}

// waitFileReady hỏi trạng thái tệp fileID mỗi imagePollEvery tới khi ready (nil), rejected (errImageRejected kèm lý do)
// hay quá wait (errImageWaitExpire). Ảnh do worker của tiến trình CRM xử lý (file.process).
func waitFileReady(ctx context.Context, d *db.DB, tenant, fileID uuid.UUID, wait time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	t := time.NewTicker(imagePollEvery)
	defer t.Stop()
	for {
		var s files.FileStatus
		if err := tenancy.WithTenant(ctx, d, tenant, func(tx pgx.Tx) error {
			var err error
			s, err = files.Status(ctx, tx, fileID)
			return err //nolint:wrapcheck // files đã bọc
		}); err != nil && ctx.Err() == nil {
			return fmt.Errorf("đọc trạng thái ảnh: %w", err)
		}
		switch s.Status {
		case files.StatusReady:
			return nil
		case files.StatusRejected:
			return fmt.Errorf("%w: %s", errImageRejected, s.RejectReason)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (%s, trạng thái %q; tiến trình CRM có đang chạy?)", errImageWaitExpire, wait, s.Status)
		case <-t.C:
		}
	}
}

// banners nhập banner trang chủ (nhóm 12d): gộp theo id vào danh sách CRM (giữ thứ tự CRM, banner website chưa có
// thêm vào cuối theo thứ tự website); mỗi banner kiểm bằng luật của content trên danh sách đã nhận tới đó; lưu một
// lần (SaveBanners theo revision). Chạy khô không lưu (ảnh chưa tải lên): chỉ kiểm luật với mã ảnh tạm.
func (im *websiteImporter) banners(ctx context.Context, tx pgx.Tx, x *wxBannerList, images map[string]bannerImage,
	c *importCount,
) error {
	if x == nil || len(x.Items) == 0 {
		return nil
	}
	cur, err := content.GetBanners(ctx, tx)
	if err != nil {
		return err //nolint:wrapcheck // content đã bọc
	}
	cats, err := loadCategoryPaths(ctx, tx)
	if err != nil {
		return err
	}
	cands, placeholders := bannerCandidates(x.Items, idSet(cur.Banners, func(b content.Banner) uuid.UUID { return b.ID }),
		cats, images, c)
	if len(cands) == 0 {
		return nil
	}
	ref, err := content.LoadRefData(ctx, tx, content.NormalizeBanners(append(slices.Clone(cur.Banners), cands...)), nil)
	if err != nil {
		return err //nolint:wrapcheck // content đã bọc
	}
	for id := range placeholders {
		ref.Images[id] = files.FileStatus{Purpose: content.BannerPurposeKey, Status: files.StatusReady, Width: 2400, Height: 1000}
	}
	list := slices.Clone(cur.Banners)
	added := 0
	for _, b := range cands {
		try := append(slices.Clone(list), b)
		if err := content.ValidateBanners(content.NormalizeBanners(try), ref); err != nil {
			c.fail(b.ID.String(), err)
			continue
		}
		list, added = try, added+1
	}
	if added == 0 || im.DryRun {
		c.Added += added
		return nil
	}
	w := content.Writer{Events: im.Events, Queue: im.Queue, Realtime: im.Bus}
	ch, err := w.SaveBanners(ctx, tx, list, cur.Revision, content.Actor{TenantID: im.Tenant, UserID: importSystemID()}, im.Now())
	if err != nil {
		return fmt.Errorf("lưu banner: %w", err)
	}
	c.Added += added
	return im.audit(ctx, tx, "content.banners.save", "content.home_banners", im.Tenant.String(),
		ch.Before.AuditFields(), ch.After.AuditFields())
}

// bannerCandidates đổi các banner website chưa có trong CRM (have) theo thứ tự position sang content.Banner; banner
// đã có → bỏ qua, đổi lỗi → lỗi bản ghi đó. Trả thêm tập mã ảnh tạm (chạy khô).
func bannerCandidates(list []wxBanner, have map[uuid.UUID]bool, cats categoryPaths, images map[string]bannerImage,
	c *importCount,
) ([]content.Banner, map[uuid.UUID]bool) {
	items := slices.Clone(list)
	slices.SortStableFunc(items, func(a, b wxBanner) int { return cmp.Compare(a.Position, b.Position) })
	var cands []content.Banner
	placeholders := map[uuid.UUID]bool{}
	for _, b := range items {
		if have[b.ID] {
			c.Skipped++
			continue
		}
		cb, ph, err := bannerImport(b, cats, images)
		if err != nil {
			c.fail(b.ID.String(), err)
			continue
		}
		for _, id := range ph {
			placeholders[id] = true
		}
		cands = append(cands, cb)
	}
	return cands, placeholders
}

// bannerImport đổi một banner của website sang content.Banner; trả thêm các mã ảnh tạm (chạy khô).
func bannerImport(b wxBanner, cats categoryPaths, images map[string]bannerImage) (content.Banner, []uuid.UUID, error) {
	out := content.Banner{
		ID: b.ID, Enabled: b.Enabled, Kind: b.Kind, Kicker: b.Kicker, Title: b.Title, CTA: b.CTA, Theme: b.Theme,
		StartsAt: b.StartsAt, EndsAt: b.EndsAt, ProductIDs: b.ProductIDs, Alt: b.Alt,
	}
	if b.CampaignID != nil {
		out.CampaignID = *b.CampaignID
	}
	if b.Category != nil {
		id, err := cats.resolve(*b.Category)
		if err != nil {
			return content.Banner{}, nil, err
		}
		out.CategoryID = id
	}
	ph, err := bannerPictures(b.Image, images, &out)
	if err != nil {
		return content.Banner{}, nil, err
	}
	if b.Link != nil {
		out.LinkKind = b.Link.Kind
		if out.LinkID, err = linkTarget(*b.Link, cats); err != nil {
			return content.Banner{}, nil, err
		}
	}
	return out, ph, nil
}

// bannerPictures gắn mã tệp CRM của ảnh desktop, mobile (đã chuẩn bị trong images) vào out; ảnh lỗi → lỗi của ảnh.
// Trả các mã ảnh tạm (chạy khô).
func bannerPictures(img *wxBannerImage, images map[string]bannerImage, out *content.Banner) ([]uuid.UUID, error) {
	if img == nil {
		return nil, nil
	}
	var ph []uuid.UUID
	for _, p := range []struct {
		pic *wxPicture
		dst *uuid.UUID
	}{{img.Desktop, &out.ImageDesktop}, {img.Mobile, &out.ImageMobile}} {
		if p.pic == nil {
			continue
		}
		got, ok := images[p.pic.File]
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: %q chưa được chuẩn bị", errBannerFile, p.pic.File)
		case got.Err != nil:
			return nil, got.Err
		}
		*p.dst = got.ID
		if got.Placeholder {
			ph = append(ph, got.ID)
		}
	}
	return ph, nil
}

// linkTarget trả mã đích của liên kết banner: chiến dịch, mẫu, hay danh mục theo đường tên; không có đích → uuid.Nil.
func linkTarget(l wxLink, cats categoryPaths) (uuid.UUID, error) {
	switch {
	case l.CampaignID != nil:
		return *l.CampaignID, nil
	case l.ProductID != nil:
		return *l.ProductID, nil
	case l.Category != nil:
		return cats.resolve(*l.Category)
	}
	return uuid.Nil, nil
}

var errLookPhoto = errors.New("ảnh người mẫu không khớp ảnh sản phẩm trong CRM")

// looks nhập bộ phối (nhóm 12d, sau banner): gộp theo id như banner; ảnh người mẫu {productId, imagePosition} → ảnh
// thứ imagePosition (đếm từ 0) trong ảnh đang hiện của mẫu theo thứ tự CRM; ảnh riêng của SKU → lỗi (chọn trong CRM).
// Mỗi bộ kiểm bằng content.PreviewLooks trên danh sách đã nhận tới đó cộng bộ đó (quá MaxHomeLooks bộ trang chủ, link
// key trùng → bộ đó lỗi). Lưu một lần (SaveLooks), cả khi chạy khô (rồi lùi).
func (im *websiteImporter) looks(ctx context.Context, tx pgx.Tx, x *wxLookList, c *importCount) error {
	if x == nil || len(x.Items) == 0 {
		return nil
	}
	cats, err := loadCategoryPaths(ctx, tx)
	if err != nil {
		return err
	}
	cur, err := content.GetLooks(ctx, tx)
	if err != nil {
		return err //nolint:wrapcheck // content đã bọc
	}
	cands, photos := lookCandidates(x.Items, idSet(cur.Looks, func(l content.Look) uuid.UUID { return l.ID }), cats, c)
	if len(cands) == 0 {
		return nil
	}
	ref, err := content.LoadLookRefData(ctx, tx, append(slices.Clone(cur.Looks), cands...), nil, im.ImageView)
	if err != nil {
		return err //nolint:wrapcheck // content đã bọc
	}
	list := slices.Clone(cur.Looks)
	added := 0
	for _, l := range cands {
		file, err := lookPhotoFile(photos[l.ID], ref)
		if err != nil {
			c.fail(l.ID.String(), err)
			continue
		}
		l.PhotoFileID = file
		// Luật cả danh sách (số bộ, số bộ trang chủ, mã và link key trùng) kiểm trên danh sách đã nhận tới đó cộng bộ
		// này: bộ phạm luật chỉ làm bộ đó lỗi, không làm hỏng lần lưu cả loại.
		ok, skipped, err := content.PreviewLooks(append(slices.Clone(list), l), ref, cur.Looks)
		if err == nil {
			err = lookSkipErr(l.ID, ok, skipped)
		}
		if err != nil {
			c.fail(l.ID.String(), err)
			continue
		}
		list, added = append(list, l), added+1
	}
	if added == 0 {
		return nil
	}
	w := content.Writer{Events: im.Events, Queue: im.Queue, Realtime: im.Bus}
	ch, err := w.SaveLooks(ctx, tx, list, cur.Revision, content.Actor{TenantID: im.Tenant, UserID: importSystemID()}, im.Now())
	if err != nil {
		return fmt.Errorf("lưu bộ phối: %w", err)
	}
	c.Added += added
	return im.audit(ctx, tx, "content.look.save", "content.looks", im.Tenant.String(), ch.Before.AuditFields(), ch.After.AuditFields())
}

// lookCandidates đổi các bộ website chưa có trong CRM (have) theo thứ tự position sang content.Look (chưa có ảnh);
// bộ đã có → bỏ qua, đổi lỗi → lỗi bộ đó. Trả thêm ảnh người mẫu của website theo mã bộ.
func lookCandidates(list []wxLook, have map[uuid.UUID]bool, cats categoryPaths, c *importCount) (
	[]content.Look, map[uuid.UUID]wxLookPhoto,
) {
	items := slices.Clone(list)
	slices.SortStableFunc(items, func(a, b wxLook) int { return cmp.Compare(a.Position, b.Position) })
	var cands []content.Look
	photos := map[uuid.UUID]wxLookPhoto{}
	for _, l := range items {
		if have[l.ID] {
			c.Skipped++
			continue
		}
		cl, err := lookImport(l, cats)
		if err != nil {
			c.fail(l.ID.String(), err)
			continue
		}
		photos[l.ID] = l.Photo
		cands = append(cands, cl)
	}
	return cands, photos
}

// lookSkipErr: bộ id có trong ok → nil; bị bỏ → lỗi luật của bộ đó.
func lookSkipErr(id uuid.UUID, ok []content.Look, skipped []content.LookSkip) error {
	for _, s := range skipped {
		if s.ID == id {
			return s.Err
		}
	}
	if !slices.ContainsFunc(ok, func(l content.Look) bool { return l.ID == id }) {
		return errors.New("luật bộ phối không nhận bộ này")
	}
	return nil
}

func lookImport(l wxLook, cats categoryPaths) (content.Look, error) {
	out := content.Look{
		ID: l.ID, OnHome: l.Enabled, Name: l.Name, Title: l.Title, Headline: l.Headline, Story: l.Story,
		ModelSize: l.ModelSize, StartsAt: l.StartsAt, EndsAt: l.EndsAt,
	}
	if l.ModelHeight != nil {
		out.ModelHeightCM = *l.ModelHeight
	}
	for _, it := range l.Items {
		out.Items = append(out.Items, content.LookItem{ProductID: it.ProductID, Color: it.Color, X: it.X, Y: it.Y})
	}
	for _, ref := range l.Categories {
		id, err := cats.resolve(ref)
		if err != nil {
			return content.Look{}, err
		}
		out.CategoryIDs = append(out.CategoryIDs, id)
	}
	if l.Photo.VariantID != nil {
		return content.Look{}, fmt.Errorf("%w: ảnh riêng của một SKU, chọn lại ảnh trong CRM", errLookPhoto)
	}
	return out, nil
}

// lookPhotoFile: ảnh thứ p.ImagePosition (từ 0) trong ảnh đang hiện của mẫu p.ProductID, theo thứ tự hiện của CRM.
func lookPhotoFile(p wxLookPhoto, ref content.LookRefData) (uuid.UUID, error) {
	var imgs []content.LookImage
	for _, img := range ref.Images {
		if img.ProductID == p.ProductID {
			imgs = append(imgs, img)
		}
	}
	slices.SortStableFunc(imgs, func(a, b content.LookImage) int { return cmp.Compare(a.Position, b.Position) })
	if p.ImagePosition < 0 || p.ImagePosition >= len(imgs) {
		return uuid.Nil, fmt.Errorf("%w: mẫu %s có %d ảnh, website chọn ảnh thứ %d (từ 0)", errLookPhoto, p.ProductID,
			len(imgs), p.ImagePosition)
	}
	return imgs[p.ImagePosition].FileID, nil
}
