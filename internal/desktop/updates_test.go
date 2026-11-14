package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/update"

	"gokeep/internal/config"
)

type fakeUpdateClient struct {
	feed        *update.Feed
	checkErr    error
	downloadErr error
	checks      atomic.Int32
	downloads   atomic.Int32
}

func (client *fakeUpdateClient) Check(context.Context) (*update.Feed, error) {
	client.checks.Add(1)
	return client.feed, client.checkErr
}

func (client *fakeUpdateClient) Download(
	_ context.Context,
	asset update.Asset,
	directory string,
	progress func(done, total int64),
) (string, error) {
	client.downloads.Add(1)
	if client.downloadErr != nil {
		return "", client.downloadErr
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	artifact := filepath.Join(directory, filepath.Base(asset.URL))
	if err := os.WriteFile(artifact, []byte("test update"), 0o600); err != nil {
		return "", err
	}
	progress(64, 128)
	progress(128, 128)
	return artifact, nil
}

func updateFeed(version, notes string) *update.Feed {
	format := update.FormatTarGz
	switch runtime.GOOS {
	case "darwin":
		format = update.FormatZIP
	case "windows":
		format = update.FormatNSIS
	}
	return &update.Feed{
		Schema:  update.SchemaVersion,
		App:     updateAppID,
		Channel: updateChannel,
		Latest: update.Release{
			Version:  version,
			NotesURL: notes,
			Assets: []update.Asset{{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Format: format,
				URL:    "https://example.com/" + version + "/GoKeep",
			}},
		},
	}
}

func waitForUpdate(t *testing.T, driver *app.Driver, model *Model, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		if err := paint(driver, model); err != nil {
			t.Fatalf("cannot paint while waiting for %s: %v", what, err)
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint after %s: %v", what, err)
	}
}

func TestAutomaticUpdateChecksAreEnabledByDefault(t *testing.T) {
	if !config.DefaultConfig().AutoCheckUpdates {
		t.Fatal("new configurations do not check for updates on startup")
	}
}

func TestBundledUpdateConfigurationIsValid(t *testing.T) {
	if _, err := newUpdateClient("0.1.1"); err != nil {
		t.Fatalf("the bundled update feed configuration is invalid: %v", err)
	}
}

func TestAnUpdateDownloadsAutomaticallyButInstallsOnlyWhenConfirmed(t *testing.T) {
	driver, model := newTestPage(t, "en")
	model.update.supported = true
	client := &fakeUpdateClient{feed: updateFeed("9.9.9", "https://example.com/notes")}
	model.update.client = client

	var installed int
	var installMu sync.Mutex
	var installOptions update.InstallOptions
	model.update.installer = func(options update.InstallOptions, _ func()) error {
		installMu.Lock()
		installed++
		installOptions = options
		installMu.Unlock()
		return nil
	}

	model.startUpdateCheck(false)
	waitForUpdate(t, driver, model, "the update to download", func() bool {
		return model.update.ready
	})

	if client.checks.Load() != 1 || client.downloads.Load() != 1 {
		t.Fatalf("check/download calls = %d/%d, want 1/1",
			client.checks.Load(), client.downloads.Load())
	}
	if installed != 0 {
		t.Fatal("the downloaded update was installed without confirmation")
	}
	if !model.update.open {
		t.Fatal("the update dialog did not open for the downloaded update")
	}
	if !model.update.available {
		t.Fatal("the downloaded update is not marked as available")
	}
	if model.update.version != "9.9.9" {
		t.Fatalf("the update version is %q, want 9.9.9", model.update.version)
	}
	if model.progress() != 1 {
		t.Fatalf("the download progress is %v, want 1", model.progress())
	}

	clickLabel(t, driver, model, model.t("updates.install"))

	installMu.Lock()
	defer installMu.Unlock()
	if installed != 1 {
		t.Fatalf("the installer was called %d times after confirmation, want 1", installed)
	}
	if installOptions.Version != "9.9.9" || installOptions.App != updateAppID {
		t.Fatalf("the installer received %+v, want version 9.9.9 for %s", installOptions, updateAppID)
	}
	if installOptions.Artifact == "" {
		t.Fatal("the installer received no artifact")
	}
	if installOptions.Asset.OS != runtime.GOOS || installOptions.Asset.Arch != runtime.GOARCH {
		t.Fatalf("the installer received asset %+v for %s/%s",
			installOptions.Asset, runtime.GOOS, runtime.GOARCH)
	}
}

func TestAnAutomaticCheckFailureStaysSilent(t *testing.T) {
	driver, model := newTestPage(t, "en")
	model.update.supported = true
	client := &fakeUpdateClient{checkErr: errors.New("offline")}
	model.update.client = client

	model.startUpdateCheck(false)
	waitForUpdate(t, driver, model, "the failed automatic check", func() bool {
		return client.checks.Load() == 1 && !model.update.checking
	})

	if model.update.open || model.update.errorKey != "" {
		t.Fatalf("a failed automatic check opened the dialog or reported an error: open=%v error=%q",
			model.update.open, model.update.errorKey)
	}
}

func TestAManualCheckReportsVerificationFailures(t *testing.T) {
	driver, model := newTestPage(t, "en")
	model.update.supported = true
	model.update.client = &fakeUpdateClient{checkErr: update.ErrChecksum}

	model.startUpdateCheck(true)
	waitForUpdate(t, driver, model, "the failed manual check", func() bool {
		return model.update.errorKey != ""
	})

	if !model.update.open {
		t.Fatal("a failed manual check did not open the update dialog")
	}
	if model.update.errorKey != "updates.errorVerification" {
		t.Fatalf("the error key is %q, want updates.errorVerification", model.update.errorKey)
	}
}

func TestUpdateErrorsUseTheSpecificTranslations(t *testing.T) {
	model := &Model{}
	cases := []struct {
		err  error
		want string
	}{
		{update.ErrManagedExternally, "updates.errorManaged"},
		{update.ErrNotWritable, "updates.errorNotWritable"},
		{update.ErrUnsupported, "updates.errorUnsupported"},
		{update.ErrSignature, "updates.errorVerification"},
		{update.ErrChecksum, "updates.errorVerification"},
		{update.ErrSize, "updates.errorVerification"},
		{update.ErrUntrusted, "updates.errorVerification"},
		{update.ErrInsecureURL, "updates.errorVerification"},
		{update.ErrNoAsset, "updates.errorNoAsset"},
		{update.ErrExpired, "updates.errorExpired"},
		{update.ErrManifest, "updates.errorVerification"},
		{update.ErrTooLarge, "updates.errorVerification"},
		{update.ErrUnsafeArchive, "updates.errorVerification"},
		{errors.New("offline"), "updates.errorGeneric"},
	}
	for _, test := range cases {
		if got := model.updateErrorKey(test.err); got != test.want {
			t.Errorf("%v maps to %q, want %q", test.err, got, test.want)
		}
	}
}
