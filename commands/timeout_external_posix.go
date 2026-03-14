//go:build !windows

package commands

import (
	"context"
	"errors"
	"io"
	osexec "os/exec"
	"syscall"
	"time"
)

const externalTimeoutKillGrace = 100 * time.Millisecond

func runExternalCompatTimeout(ctx context.Context, inv *Invocation, timeout time.Duration, argv []string) (int, string, error) {
	if inv == nil {
		return 0, "", errors.New("timeout: invocation missing")
	}
	if len(argv) == 0 {
		return 0, "", errors.New("timeout: missing operand")
	}
	resolved, ok, err := resolveCommand(ctx, inv, inv.Env, inv.Cwd, argv[0])
	if err != nil {
		return 0, "", err
	}
	if !ok {
		return 127, "", nil
	}
	return runExternalTimeoutProcess(ctx, timeout, resolved.Path, argv[1:], inv.Env, inv.Cwd, inv.Stdin, inv.Stdout, inv.Stderr)
}

func runExternalTimeoutProcess(ctx context.Context, timeout time.Duration, path string, argv []string, env map[string]string, cwd string, stdin io.Reader, stdout, stderr io.Writer) (int, string, error) {
	cmd := osexec.Command(path, argv...)
	cmd.Dir = cwd
	cmd.Env = sortedEnvPairs(env)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 0, "", err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var timeoutC <-chan time.Time
	var timer *time.Timer
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		timeoutC = timer.C
		defer timer.Stop()
	}

	select {
	case err := <-done:
		return externalTimeoutExitCode(cmd, err), "", nil
	case <-timeoutC:
		terminateExternalTimeoutProcessGroup(cmd, done)
		return 124, timeoutControlMessage(timeout), nil
	case <-ctx.Done():
		terminateExternalTimeoutProcessGroup(cmd, done)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return 124, timeoutControlMessage(timeout), nil
		}
		return 130, "execution canceled", nil
	}
}

func terminateExternalTimeoutProcessGroup(cmd *osexec.Cmd, done <-chan error) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
		return
	case <-time.After(externalTimeoutKillGrace):
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	<-done
}

func externalTimeoutExitCode(cmd *osexec.Cmd, err error) int {
	if err == nil {
		if cmd != nil && cmd.ProcessState != nil {
			return cmd.ProcessState.ExitCode()
		}
		return 0
	}
	var exitErr *osexec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ProcessState != nil {
			if code := exitErr.ProcessState.ExitCode(); code >= 0 {
				return code
			}
		}
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
	}
	return 1
}
