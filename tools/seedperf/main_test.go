package main

import (
	"errors"
	"log/slog"
	"testing"
)

func quietLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestRun_BadFlags(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{},
		{"-admin", "postgres://x"},
		{"-admin", "postgres://x", "-out", "f", "-customers", "0"},
		{"-admin", "postgres://x", "-out", "f", "-extra-phone-permille", "1001"},
		{"-admin", "postgres://x", "-out", "f", "-customers", "9000000"},
	} {
		if err := run(t.Context(), args, quietLogger()); !errors.Is(err, errUsage) {
			t.Errorf("%v: %v, muốn errUsage", args, err)
		}
	}
}
