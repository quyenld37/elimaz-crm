package imager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// Runner chạy một chương trình con và trả stdout. Bản thật là ExecRunner; test cắm bản giả để đếm lần gọi.
// memLimit > 0: giới hạn không gian địa chỉ (byte) của tiến trình con. Hết hạn ctx thì tiến trình con bị giết.
type Runner interface {
	Run(ctx context.Context, name string, args []string, memLimit int64) ([]byte, error)
}

const (
	// maxStdout: vipsheader -f in một con số hay một tên bộ đọc; nhiều hơn là bất thường.
	maxStdout = 64 << 10
	maxStderr = 4 << 10
	// waitDelay: sau khi giết nhóm, chờ ống stdout/stderr đóng tối đa chừng này rồi bỏ.
	waitDelay = time.Second
)

var (
	errOutputTooLarge = errors.New("stdout của tiến trình con quá giới hạn")
	errChildFailed    = errors.New("tiến trình con lỗi")
)

// ExecRunner chạy chương trình bằng os/exec: nhóm tiến trình riêng (Setpgid), quá hạn thì SIGKILL cả nhóm,
// giới hạn bộ nhớ bằng `prlimit --as`, môi trường tối thiểu (không thừa hưởng biến của imager).
type ExecRunner struct {
	Prlimit string   // đường dẫn prlimit (util-linux)
	Env     []string // toàn bộ môi trường của tiến trình con
}

// NewExecRunner trả ExecRunner với môi trường tối thiểu cộng extra (dạng "TÊN=giá trị").
// VIPS_BLOCK_UNTRUSTED=1 chặn các bộ đọc libvips không dành cho dữ liệu lạ (ImageMagick, Matlab, …);
// MALLOC_ARENA_MAX=2 để giới hạn --as không bị ăn hết bởi vùng nhớ dự trữ của mỗi luồng.
func NewExecRunner(extra []string) ExecRunner {
	env := []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"LANG=C.UTF-8",
		"VIPS_BLOCK_UNTRUSTED=1",
		"MALLOC_ARENA_MAX=2",
	}
	return ExecRunner{Prlimit: "prlimit", Env: append(env, extra...)}
}

// Run chạy name với args. Lỗi bọc ctx.Err() khi bị giết vì hết hạn, để bên gọi phân biệt quá giờ với lỗi giải mã.
func (r ExecRunner) Run(ctx context.Context, name string, args []string, memLimit int64) ([]byte, error) {
	if memLimit > 0 {
		args = append([]string{"--as=" + strconv.FormatInt(memLimit, 10), "--", name}, args...)
		name = r.Prlimit
	}
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: chương trình và tham số do imager dựng, không lấy từ yêu cầu
	cmd.Env = r.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Âm: gửi cho cả nhóm (script con, luồng con của libvips không lọt).
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = waitDelay
	stdout := &capped{limit: maxStdout}
	stderr := &capped{limit: maxStderr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	// Chỉ giết nhóm trong Cancel (lúc tiến trình đầu chưa được gặt, số nhóm chắc chắn còn là của nó). Giết sau Wait
	// thì số đó có thể đã cấp lại cho tiến trình con của một yêu cầu khác đang chạy song song.
	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("chạy %s: %w", name, ctxErr)
	}
	if err != nil {
		return nil, fmt.Errorf("chạy %s: %w: %w (%s)", name, errChildFailed, err, bytes.TrimSpace(stderr.buf.Bytes()))
	}
	if stdout.over {
		return nil, fmt.Errorf("chạy %s: %w", name, errOutputTooLarge)
	}
	return stdout.buf.Bytes(), nil
}

// capped giữ tối đa limit byte, bỏ phần thừa và ghi nhận đã tràn (tiến trình con không bị chặn vì ống đầy).
type capped struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (c *capped) Write(p []byte) (int, error) {
	room := c.limit - c.buf.Len()
	if len(p) > room {
		c.over = true
		if room > 0 {
			c.buf.Write(p[:room])
		}
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}
