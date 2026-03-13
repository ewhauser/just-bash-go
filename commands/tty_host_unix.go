//go:build unix

package commands

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func ttyHostPath(file *os.File) (string, bool) {
	if file == nil || !term.IsTerminal(int(file.Fd())) {
		return "", false
	}

	for _, candidate := range []string{
		strings.TrimSpace(file.Name()),
		filepath.Join("/proc/self/fd", itoaFD(file.Fd())),
		filepath.Join("/dev/fd", itoaFD(file.Fd())),
	} {
		if ttyPath, ok := ttyHostCandidatePath(candidate); ok {
			return ttyPath, true
		}
	}

	if ttyPath, ok := ttyHostPathFromDevice(file); ok {
		return ttyPath, true
	}

	return "/dev/tty", true
}

func ttyHostCandidatePath(candidate string) (string, bool) {
	if ttyPath, ok := ttyRecognizedPath(filepath.ToSlash(strings.TrimSpace(candidate))); ok {
		return ttyPath, true
	}

	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", false
	}
	return ttyRecognizedPath(filepath.ToSlash(resolved))
}

func ttyHostPathFromDevice(file *os.File) (string, bool) {
	var target unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &target); err != nil {
		return "", false
	}

	var match string
	_ = filepath.WalkDir("/dev", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}

		info, statErr := os.Stat(name)
		if statErr != nil {
			return nil
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !ttySameDevice(&target, stat) {
			return nil
		}

		if ttyPath, ok := ttyRecognizedPath(filepath.ToSlash(name)); ok {
			match = ttyPath
			return fs.SkipAll
		}
		return nil
	})

	if match == "" {
		return "", false
	}
	return match, true
}

func ttySameDevice(target *unix.Stat_t, stat *syscall.Stat_t) bool {
	if target == nil || stat == nil {
		return false
	}
	return uint64(target.Dev) == uint64(stat.Dev) &&
		uint64(target.Ino) == uint64(stat.Ino) &&
		uint64(target.Rdev) == uint64(stat.Rdev)
}

func itoaFD(fd uintptr) string {
	return strconv.FormatUint(uint64(fd), 10)
}
