package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Savepoint chạy fn trong một savepoint của tx (transaction đang mở bằng RetryTx, tenancy.WithTenant hay của worker):
// fn trả lỗi → chỉ phần fn đã ghi bị lùi (ROLLBACK TO SAVEPOINT, kể cả khi một câu SQL làm transaction hỏng) và trả
// nguyên lỗi của fn; tx dùng tiếp được. fn không lỗi → nhả savepoint, phần ghi ở lại tới khi tx commit hay lùi.
//
// Khi nào dùng: một bước "tất cả hoặc không" bên trong transaction lớn mà lỗi nghiệp vụ của bước đó phải được ghi
// lại trong chính transaction (ví dụ áp bản nhập: lỗi → lùi phần đã ghi, đặt bản nhập failed). Khi nào không dùng:
// thay cho RetryTx (savepoint không mở transaction mới, không chạy lại lỗi tạm); bắt lỗi tạm 40P01, 55P03 để làm tiếp
// (lỗi tạm phải trả lên cho RetryTx hay hàng đợi chạy lại cả transaction).
func Savepoint(ctx context.Context, tx pgx.Tx, fn func(pgx.Tx) error) error {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("mở savepoint: %w", err)
	}
	if err := fn(sp); err != nil {
		if rerr := sp.Rollback(ctx); rerr != nil {
			return fmt.Errorf("lùi savepoint: %w", errors.Join(err, rerr))
		}
		return err
	}
	if err := sp.Commit(ctx); err != nil {
		return fmt.Errorf("nhả savepoint: %w", err)
	}
	return nil
}
