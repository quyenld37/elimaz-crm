package files

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// findDuplicate tìm tệp ready khác cùng tenant, cùng mục đích, cùng nội dung, cùng bộ kích thước (transaction đọc
// ngắn). Cùng mục đích vì mục đích khác có thể cho loại tệp khác (Q7); biến thể riêng tư là khóa của tệp cùng
// tenant nên không bao giờ dùng chung giữa tenant (R10). Không có → uuid.Nil.
func (p *Processor) findDuplicate(ctx context.Context, jb job, sumHex string) (uuid.UUID, error) {
	sum, err := hex.DecodeString(sumHex)
	if err != nil {
		return uuid.Nil, fmt.Errorf("tìm tệp trùng: %w", err)
	}
	var id uuid.UUID
	err = tenancy.WithTenant(ctx, p.o.DB, jb.tenant, func(tx pgx.Tx) error {
		var err error
		id, err = store.New(tx).FindReadyDuplicate(ctx, store.FindReadyDuplicateParams{
			TenantID: jb.tenant, ContentSha256: sum, Purpose: jb.purpose.Key, PresetVersion: int4(p.o.PresetVersion),
			ID: jb.fileID,
		})
		return err //nolint:wrapcheck // xử lý ở dưới
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, nil
	case err != nil:
		return uuid.Nil, fmt.Errorf("tìm tệp trùng: %w", err)
	}
	return id, nil
}

// reuseResult dựng kết quả từ tệp trùng đã ready (trong transaction của OnDone): kích thước, loại, mã băm, và các
// dòng biến thể của đúng bộ kích thước của nó (cùng khóa trên kho).
func reuseResult(ctx context.Context, s *store.Queries, reuse string) (processResult, error) {
	id, err := uuid.Parse(reuse)
	if err != nil {
		return processResult{}, fmt.Errorf("tệp trùng %q: %w", reuse, err)
	}
	src, err := s.GetFile(ctx, id)
	if err != nil {
		return processResult{}, fmt.Errorf("đọc tệp trùng %s: %w", id, err)
	}
	vs, err := s.ListFileVariantsFull(ctx, id)
	if err != nil {
		return processResult{}, fmt.Errorf("đọc biến thể của tệp trùng %s: %w", id, err)
	}
	res := processResult{
		SHA256: hex.EncodeToString(src.ContentSha256), DetectedType: src.DetectedType.String,
		Width: int(src.Width.Int32), Height: int(src.Height.Int32), Preset: int(src.PresetVersion.Int32),
	}
	for _, v := range vs {
		if v.PresetVersion == src.PresetVersion.Int32 {
			res.Variants = append(res.Variants, variantResult{Width: int(v.Width), Format: v.Format, Key: v.Key, Size: v.SizeBytes})
		}
	}
	return res, nil
}
