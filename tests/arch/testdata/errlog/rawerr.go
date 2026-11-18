package errlog

import (
	"context"
	"log/slog"
)

// Mẫu vi phạm: nội dung lỗi thô vào log.
func raw(ctx context.Context, l *slog.Logger, err error) {
	l.Error("a", slog.String("error", err.Error()))
	l.ErrorContext(ctx, "b", "error", err.Error())
	l.LogAttrs(ctx, slog.LevelError, "c", slog.Any("error", err))
	slog.Warn("d", "err", err)
}
