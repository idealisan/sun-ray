//go:build !windows

package main

// tun_helper_launch_unix.go starts the helper through osascript on macOS
// and pkexec on Linux, so the user gets the system's own administrator
// prompt instead of having to launch Sun Ray as root.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func helperElevated() bool { return os.Geteuid() == 0 }

func launchTunHelper(exe, configPath, errorPath string) error {
	if helperElevated() {
		cmd := exec.Command(exe, tunHelperArg, "-config", configPath, "-errorfile", errorPath)
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}

	dir, err := os.MkdirTemp("", "sunray-tun-run-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	scriptPath := filepath.Join(dir, "run.sh")
	var script string
	switch runtime.GOOS {
	case "darwin":
		// Do not start the helper as a child of "do shell script": macOS
		// may wait for or kill background descendants when that command
		// exits. launchd owns the process instead, so it survives and can
		// connect back to our control listener.
		script = "#!/bin/sh\n" +
			"/bin/launchctl remove " + tunHelperJobLabel + " >/dev/null 2>&1 || true\n" +
			"exec /bin/launchctl submit -l " + tunHelperJobLabel +
			" -o /dev/null -e " + shellQuote(errorPath) +
			" -- " + shellQuote(exe) + " " + tunHelperArg +
			" -config " + shellQuote(configPath) +
			" -errorfile " + shellQuote(errorPath) + "\n"
	case "linux":
		script = "#!/bin/sh\nnohup " +
			shellQuote(exe) + " " + tunHelperArg +
			" -config " + shellQuote(configPath) +
			" -errorfile " + shellQuote(errorPath) +
			" >/dev/null 2>&1 &\n"
	default:
		return errors.New("TUN 辅助进程在 " + runtime.GOOS + " 上还不支持")
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return err
	}

	switch runtime.GOOS {
	case "darwin":
		apple := appleScriptQuote(shellQuote(scriptPath))
		cmd := exec.Command("/usr/bin/osascript", "-e",
			fmt.Sprintf(`do shell script "/bin/sh %s" with administrator privileges`, apple))
		out, err := cmd.CombinedOutput()
		if err != nil {
			text := strings.TrimSpace(string(out))
			if strings.Contains(strings.ToLower(text), "cancel") || strings.Contains(text, "取消") {
				return errElevationCancelled
			}
			return fmt.Errorf("osascript: %v: %s", err, text)
		}
		return nil
	case "linux":
		cmd := exec.Command("pkexec", "/bin/sh", scriptPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			text := strings.TrimSpace(string(out))
			if strings.Contains(strings.ToLower(text), "dismiss") || strings.Contains(text, "取消") {
				return errElevationCancelled
			}
			return fmt.Errorf("pkexec: %v: %s", err, text)
		}
		return nil
	}
	return errors.New("TUN 辅助进程在 " + runtime.GOOS + " 上还不支持")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func appleScriptQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}
