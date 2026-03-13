//go:build !unix

package commands

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

func ttyHostPath(file *os.File) (string, bool) {
	if file == nil || !term.IsTerminal(int(file.Fd())) {
		return "", false
	}

	if ttyPath, ok := ttyRecognizedPath(filepath.ToSlash(strings.TrimSpace(file.Name()))); ok {
		return ttyPath, true
	}

	name := strings.TrimSpace(file.Name())
	if name == "" {
		name = "CONIN$"
	}
	return filepath.ToSlash(name), true
}
