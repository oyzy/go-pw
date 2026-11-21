//go:build windows

package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/vivaui/viva/update"
)

const (
	// createNoWindow is CREATE_NO_WINDOW: the helper and the installer run
	// without a console of their own.
	createNoWindow = 0x08000000

	// updateHelperSeconds is how long the helper waits for this process to end
	// before it installs anyway. A normal quit takes well under a second.
	updateHelperSeconds = 90
)

// installUpdate installs a downloaded Windows package through a helper the Task
// Scheduler owns.
//
// The application runs in a Windows job object, and the job ends with the
// process: a helper started as its child is terminated at the very moment the
// installer should run. A scheduled task belongs to the Task Scheduler service
// instead, so it outlives the application and can wait for it to close, run the
// NSIS installer silently and start the new version.
func installUpdate(options update.InstallOptions, quit func()) error {
	if options.Asset.Format != update.FormatNSIS {
		return update.Install(options, quit)
	}
	if quit == nil {
		return errors.New("update: install needs a quit function")
	}
	if options.Version == "" {
		return errors.New("update: install needs the version it installs")
	}
	if _, err := update.Compare(options.Version, options.Version); err != nil {
		return fmt.Errorf("update: version %q: %w", options.Version, err)
	}
	artifact, err := filepath.Abs(options.Artifact)
	if err != nil {
		return fmt.Errorf("update: resolve %s: %w", options.Artifact, err)
	}
	info, err := os.Stat(artifact)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("update: %s is not a regular file", artifact)
	}
	if !strings.EqualFold(filepath.Ext(artifact), ".exe") {
		return fmt.Errorf("update: %s is not an .exe installer", artifact)
	}
	if options.Asset.Size > 0 && info.Size() != options.Asset.Size {
		return fmt.Errorf("update: %s is %d bytes, the feed declared %d", artifact, info.Size(), options.Asset.Size)
	}
	if len(options.Asset.SHA256) == 64 {
		digest, err := fileSHA256(artifact)
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
		if !strings.EqualFold(digest, options.Asset.SHA256) {
			return fmt.Errorf("update: %s does not match the feed checksum", artifact)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("update: locate the running program: %w", err)
	}
	if executable, err = filepath.Abs(executable); err != nil {
		return fmt.Errorf("update: resolve %s: %w", executable, err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("update: locate the configuration directory: %w", err)
	}
	script, err := writeUpdateHelper(filepath.Join(configDir, appTitle, "updates"), artifact, executable, options.Args)
	if err != nil {
		return err
	}
	task := fmt.Sprintf("GoKeepUpdate-%d", os.Getpid())
	slog.Info("installing the update", "version", options.Version, "artifact", artifact)
	if err := registerUpdateTask(task, script); err != nil {
		_ = os.Remove(script)
		return err
	}
	if err := runUpdateTask(task); err != nil {
		_ = deleteUpdateTask(task)
		_ = os.Remove(script)
		return err
	}
	slog.Info("the update helper is scheduled", "version", options.Version, "task", task)
	quit()
	return nil
}

// fileSHA256 returns the hex SHA-256 of path.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// writeUpdateHelper stores the script the scheduled task runs and returns its path.
// The script waits for this process, installs silently and starts the new version.
func writeUpdateHelper(directory, artifact, executable string, args []string) (string, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("update: prepare the helper directory: %w", err)
	}
	scriptPath := filepath.Join(directory, fmt.Sprintf("install-%d.ps1", os.Getpid()))
	logPath := filepath.Join(directory, "install.log")
	task := fmt.Sprintf("GoKeepUpdate-%d", os.Getpid())
	commandLine := commandLineArgument(executable)
	for _, argument := range args {
		commandLine += " " + commandLineArgument(argument)
	}
	// Explorer, not this helper, starts the new version: a process the helper
	// starts directly would belong to the task's job and end with the task.
	start := fmt.Sprintf(`Start-Process -FilePath 'explorer.exe' -ArgumentList %s`, powerShellQuote(commandLine))
	script := strings.Join([]string{
		"$ErrorActionPreference = 'SilentlyContinue'",
		fmt.Sprintf("$deadline = (Get-Date).AddSeconds(%d)", updateHelperSeconds),
		fmt.Sprintf("$pidToWait = %d", os.Getpid()),
		"while ((Get-Date) -lt $deadline) {",
		"  if (-not (Get-Process -Id $pidToWait -ErrorAction SilentlyContinue)) { break }",
		"  Start-Sleep -Milliseconds 250",
		"}",
		"Start-Sleep -Milliseconds 700",
		fmt.Sprintf("$installer = Start-Process -FilePath '%s' -ArgumentList '/S' -Wait -PassThru", powerShellEscape(artifact)),
		fmt.Sprintf(`Add-Content -LiteralPath '%s' -Value "$(Get-Date -Format o) installed exit=$($installer.ExitCode)"`, powerShellEscape(logPath)),
		"Start-Sleep -Milliseconds 700",
		start,
		fmt.Sprintf(`Add-Content -LiteralPath '%s' -Value "$(Get-Date -Format o) restarted"`, powerShellEscape(logPath)),
		"Start-Sleep -Seconds 3",
		fmt.Sprintf("Remove-Item -LiteralPath '%s' -Force", powerShellEscape(scriptPath)),
		fmt.Sprintf("schtasks.exe /delete /tn '%s' /f", task),
		"",
	}, "\r\n")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return "", fmt.Errorf("update: write the helper: %w", err)
	}
	return scriptPath, nil
}

// registerUpdateTask creates the one-time interactive task that runs script.
func registerUpdateTask(task, script string) error {
	program := fmt.Sprintf(`powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -File "%s"`, script)
	output, err := runHidden("schtasks.exe", "/create", "/tn", task, "/tr", program, "/sc", "once", "/st", "00:00", "/it", "/f")
	if err != nil {
		return fmt.Errorf("update: create the scheduled task: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

// runUpdateTask starts the task at once and waits until the Task Scheduler has
// accepted the run.
func runUpdateTask(task string) error {
	output, err := runHidden("schtasks.exe", "/run", "/tn", task)
	if err != nil {
		return fmt.Errorf("update: start the scheduled task: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

// deleteUpdateTask removes a task that could not be started.
func deleteUpdateTask(task string) error {
	_, err := runHidden("schtasks.exe", "/delete", "/tn", task, "/f")
	return err
}

// runHidden runs a console program without a window and returns its combined output.
func runHidden(name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	output, err := command.CombinedOutput()
	return string(output), err
}

// powerShellEscape quotes a path for a single-quoted PowerShell literal.
func powerShellEscape(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

// powerShellQuote returns value as a PowerShell single-quoted string.
func powerShellQuote(value string) string {
	return "'" + powerShellEscape(value) + "'"
}

// commandLineArgument quotes one argument the way Windows parses a command line.
func commandLineArgument(argument string) string {
	if argument == "" {
		return `""`
	}
	if !strings.ContainsAny(argument, " \t\n\v\"") {
		return argument
	}
	var quoted strings.Builder
	quoted.WriteByte('"')
	backslashes := 0
	for _, character := range argument {
		switch character {
		case '\\':
			backslashes++
		case '"':
			quoted.WriteString(strings.Repeat(`\`, backslashes*2+1))
			quoted.WriteByte('"')
			backslashes = 0
		default:
			quoted.WriteString(strings.Repeat(`\`, backslashes))
			quoted.WriteRune(character)
			backslashes = 0
		}
	}
	quoted.WriteString(strings.Repeat(`\`, backslashes*2))
	quoted.WriteByte('"')
	return quoted.String()
}
