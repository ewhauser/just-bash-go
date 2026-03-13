//go:build !windows

package main

import (
	"os/exec"
	"testing"
)

func TestConfigureIsolatedProcessGroupStartsNewSession(t *testing.T) {
	cmd := exec.Command("sh", "-c", "true")

	configureIsolatedProcessGroup(cmd)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		t.Fatalf("configureIsolatedProcessGroup() did not enable Setsid")
	}
}
