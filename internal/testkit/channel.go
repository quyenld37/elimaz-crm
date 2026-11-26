package testkit

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ChannelAccount chèn (bằng chủ bảng, như Task 149 sẽ nối Page) một tài khoản kênh channelKey (facebook, fakechat)
// mã Page externalID của tenant, thuộc người bán own của tenant (tạo nếu chưa có), với mọi cột mặc định (read_only,
// active, version 1, chưa có token); trả mã tài khoản (kế hoạch M4, Task 145).
func (s *Staff) ChannelAccount(t testing.TB, tenant uuid.UUID, channelKey, externalID, name string) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	var id uuid.UUID
	if err := s.TryExec(ctx, tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO catalog.sellers (tenant_id, code, name, kind) VALUES ($1, 'ELIMAZ', 'ELIMAZ', 'own')
			ON CONFLICT (tenant_id, code) DO NOTHING`, tenant); err != nil {
			return err //nolint:wrapcheck // lỗi dựng dữ liệu thử
		}
		return tx.QueryRow(ctx, `INSERT INTO channel.channel_accounts (tenant_id, seller_id, channel, external_id, name)
			SELECT $1, id, $2, $3, $4 FROM catalog.sellers WHERE tenant_id = $1 AND code = 'ELIMAZ' RETURNING id`,
			tenant, channelKey, externalID, name).Scan(&id)
	}); err != nil {
		t.Fatalf("testkit: chèn tài khoản kênh: %v", err)
	}
	return id
}
