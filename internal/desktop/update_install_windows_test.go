//go:build windows

package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheWindowsUpdateHelperWaitsThenInstallsAndLaunchesThroughExplorer(t *testing.T) {
	directory := t.TempDir()
	artifact := filepath.Join(directory, "GoKeep 0.1.3's setup.exe")
	executable := filepath.Join(directory, "GoKeep's folder", "GoKeep.exe")
	script, err := writeUpdateHelper(directory, artifact, executable, []string{"D:\\data\\my vault.gkdb"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"$pidToWait = ",
		"Get-Process -Id $pidToWait",
		"-ArgumentList '/S' -Wait",
		"$installer.ExitCode",
		"install.log",
		"Start-Process -FilePath 'explorer.exe'",
		"0.1.3''s setup.exe",
		"GoKeep''s folder",
		"schtasks.exe /delete",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the helper does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "0.1.3's setup.exe'") || strings.Contains(text, "GoKeep's folder") {
		t.Error("the helper leaves a single quote in a PowerShell literal unescaped")
	}
}

func TestACommandLineArgumentKeepsItsSpacesAndQuotes(t *testing.T) {
	cases := map[string]string{
		"plain":                      "plain",
		"two words":                  `"two words"`,
		`a"b`:                        `"a\"b"`,
		`C:\path with space\app.exe`: `"C:\path with space\app.exe"`,
	}
	for argument, want := range cases {
		if got := commandLineArgument(argument); got != want {
			t.Errorf("commandLineArgument(%q) = %q, want %q", argument, got, want)
		}
	}
}
