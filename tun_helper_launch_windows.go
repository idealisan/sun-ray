//go:build windows

package main

// tun_helper_launch_windows.go starts the helper with ShellExecute's runas
// verb, which shows the normal UAC prompt.

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func helperElevated() bool { return isElevated() }

func launchTunHelper(exe, configPath, errorPath string) error {
	if helperElevated() {
		cmd := exec.Command(exe, tunHelperArg, "-config", configPath, "-errorfile", errorPath)
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}

	args := fmt.Sprintf(`%s -config "%s" -errorfile "%s"`, tunHelperArg, configPath, errorPath)
	err := windows.ShellExecute(0,
		windows.StringToUTF16Ptr("runas"),
		windows.StringToUTF16Ptr(exe),
		windows.StringToUTF16Ptr(args),
		windows.StringToUTF16Ptr(filepath.Dir(exe)),
		windows.SW_HIDE,
	)
	if err == windows.ERROR_CANCELLED {
		return errElevationCancelled
	}
	return err
}
