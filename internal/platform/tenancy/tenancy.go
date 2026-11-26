// Package tenancy đặt ngữ cảnh tenant cho transaction (WithTenant), tìm tenant từ khóa ngoài (Resolve,
// ListTenantIDs) và kiểm quy tắc schema của bảng nghiệp vụ (CheckSchemaRules).
//
// Bảng nghiệp vụ bật và ép RLS bằng platform.enable_tenant_rls (migration 0002): policy so tenant_id với
// app.tenant_id qua NULLIF, nên ngoài WithTenant mọi câu trên bảng đó trả 0 dòng, kể cả trên kết nối vừa dùng cho
// tenant khác.
package tenancy

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// ErrNoTenant báo WithTenant được gọi với uuid.Nil (thiếu tenant là lỗi của bên gọi, không chạy hàm).
var ErrNoTenant = errors.New("tenancy: thiếu tenant")

// settingName là tham số theo transaction mà policy tenant_isolation đọc.
const settingName = "app.tenant_id"

// WithTenant chạy fn trong một transaction của d.RetryTx (ADR-46) với app.tenant_id = tenantID. Chỉ o[0] được dùng
// (không có thì DefaultTx của pool); app.tenant_id được thêm vào cuối Settings của o[0] (bản sao, không sửa của bên
// gọi), nên được đặt cùng câu set_config với hai giới hạn thời gian ở mỗi lần chạy lại, và thắng một app.tenant_id
// khác trong Settings. Lỗi tạm thời được chạy lại; hết lượt trả lỗi bọc db.ErrRetryLater.
//
// Như RetryTx, fn có thể chạy tới 4 lần nên phải chạy lại được: không gọi mạng, không cộng dồn vào biến bên ngoài.
func WithTenant(ctx context.Context, d *db.DB, tenantID uuid.UUID, fn func(pgx.Tx) error, o ...db.TxOptions) error {
	if tenantID == uuid.Nil {
		return ErrNoTenant
	}
	var opt db.TxOptions
	if len(o) > 0 {
		opt = o[0]
	}
	opt.Settings = append(append([]db.Setting(nil), opt.Settings...), db.Setting{Name: settingName, Value: tenantID.String()})
	return d.RetryTx(ctx, opt, fn) //nolint:wrapcheck // lỗi của RetryTx và của fn đã có ngữ cảnh, trả nguyên
}

// SetTenant đặt app.tenant_id của transaction đang mở tx thành tenantID (uuid.Nil: bỏ đặt, mọi bảng có RLS trả 0
// dòng), set_config(…, true) nên hết hiệu lực khi tx kết thúc.
//
// Khi nào không dùng: việc của một tenant dùng WithTenant (hoặc việc hàng đợi có tenant). Chỉ cho việc nền toàn hệ
// thống phải chạm lần lượt nhiều tenant trong một transaction (xóa dữ liệu theo yêu cầu của Meta, Task 179); bên gọi
// bỏ đặt trước khi ghi bảng nền tảng.
func SetTenant(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) error {
	val := ""
	if tenantID != uuid.Nil {
		val = tenantID.String()
	}
	if err := store.New(tx).SetTxConfig(ctx, store.SetTxConfigParams{Names: []string{settingName}, Vals: []string{val}}); err != nil {
		return fmt.Errorf("đặt tenant của transaction: %w", err)
	}
	return nil
}
