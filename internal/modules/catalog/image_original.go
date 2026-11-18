package catalog

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
	"elimaz.com.vn/crm/internal/platform/files"
)

// OriginalImageURL ký đường dẫn tải ảnh gốc (hạn files.OriginalTTL, TP05) của ảnh đang hiện imageID thuộc mẫu
// productID của người bán sellerID; trình duyệt tải về tên "<mã mẫu>-<thứ tự hiện>.<đuôi>" (thứ tự 1, 2, … trong các
// ảnh đang hiện). Không gọi mạng. Bên gọi kiểm quyền catalog.manage trước. Lỗi: ErrNotFound (mẫu, ảnh không có, đã
// xóa, của người bán hay tenant khác; tệp đã xóa, mất trên kho hay hết hạn), ErrFileNotReady (tệp chưa sẵn sàng).
func OriginalImageURL(ctx context.Context, tx pgx.Tx, fs files.FileStore, sellerID, productID, imageID uuid.UUID) (files.SignedURL, error) {
	row, err := store.New(tx).ProductImageOriginal(ctx, store.ProductImageOriginalParams{
		ProductID: productID, ImageID: imageID, SellerID: sellerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return files.SignedURL{}, ErrNotFound
	}
	if err != nil {
		return files.SignedURL{}, fmt.Errorf("đọc ảnh của mẫu: %w", err)
	}
	su, err := files.OriginalURL(ctx, tx, fs, row.FileID, files.OriginalTTL, row.Code+"-"+strconv.Itoa(int(row.Rank)))
	switch {
	case errors.Is(err, files.ErrNotFound), errors.Is(err, files.ErrExpired):
		return files.SignedURL{}, ErrNotFound
	case errors.Is(err, files.ErrNotReady), errors.Is(err, files.ErrRejected):
		return files.SignedURL{}, ErrFileNotReady
	case err != nil:
		return files.SignedURL{}, fmt.Errorf("ký đường dẫn ảnh gốc: %w", err)
	}
	return su, nil
}
