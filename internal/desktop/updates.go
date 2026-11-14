package desktop

import (
	"context"
	"crypto/ed25519"
	"errors"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/update"
)

const (
	updateFeedURL   = "https://github.com/oyzy/go-pw/releases/latest/download/latest.json"
	updateAppID     = "dev.gokeep.app"
	updateChannel   = "stable"
	updatePublicKey = "Cegtdr5/uGDKaBR05Lfe482RqESq0th4BTSpSd7YCAE="
)

// updateClient is the part of Viva's signed update client the page uses. Tests
// replace it with a value that never reaches the network.
type updateClient interface {
	Check(context.Context) (*update.Feed, error)
	Download(context.Context, update.Asset, string, func(done, total int64)) (string, error)
}

type updateCheckKey struct {
	Generation uint64
}

type updateCheckResult struct {
	feed *update.Feed
}

type updateDownloadKey struct {
	Generation uint64
	Version    string
	Asset      update.Asset
	Directory  string
}

type updateDownloadResult struct {
	artifact string
	version  string
	asset    update.Asset
}

// updateState is the software update dialog and the work behind it. The
// progress fields are atomic because Viva reports download progress from the
// worker goroutine while the page reads it on the window thread.
type updateState struct {
	supported bool
	open      bool

	checking bool
	manual   bool
	checkSeq uint64
	checkKey updateCheckKey

	available   bool
	downloading bool
	ready       bool
	installing  bool
	version     string
	notesURL    string
	artifact    string
	asset       update.Asset
	errorKey    string

	downloadSeq uint64
	downloadKey updateDownloadKey

	progressBits     atomic.Uint32
	lastProgressWake atomic.Int64

	client         updateClient
	installer      func(update.InstallOptions, func()) error
	checkEffect    *ui.EffectState[updateCheckResult, updateCheckKey]
	downloadEffect *ui.EffectState[updateDownloadResult, updateDownloadKey]
}

// initUpdates wires the signed update feed and the two effects that run off the
// window thread. The public key is not secret; the matching private key signs
// feeds in the release workflow.
func (m *Model) initUpdates(version string) {
	m.update.supported = selfUpdatable()
	m.update.installer = update.Install
	if m.update.supported {
		client, err := newUpdateClient(version)
		if err != nil {
			slog.Error("automatic updates are not configured", "error", err)
		} else {
			m.update.client = client
		}
	}
	m.update.checkEffect = ui.NewEffectKeyed(func(ctx context.Context, _ updateCheckKey) (updateCheckResult, error) {
		if m.update.client == nil {
			return updateCheckResult{}, errors.New("updates are not configured")
		}
		feed, err := m.update.client.Check(ctx)
		return updateCheckResult{feed: feed}, err
	})
	m.update.downloadEffect = ui.NewEffectKeyed(func(ctx context.Context, key updateDownloadKey) (updateDownloadResult, error) {
		if m.update.client == nil {
			return updateDownloadResult{}, errors.New("updates are not configured")
		}
		artifact, err := m.update.client.Download(ctx, key.Asset, key.Directory, m.updateProgress)
		return updateDownloadResult{artifact: artifact, version: key.Version, asset: key.Asset}, err
	})
}

func newUpdateClient(version string) (*update.Client, error) {
	key, err := update.ParsePublicKey(updatePublicKey)
	if err != nil {
		return nil, err
	}
	return update.New(update.Config{
		FeedURL:    updateFeedURL,
		PublicKeys: []ed25519.PublicKey{key},
		App:        updateAppID,
		Channel:    updateChannel,
		Current:    version,
	})
}

// updateEffects declares the background work. A result is taken before the
// next frame, so the page paints the completed state rather than a stale one.
func (m *Model) updateEffects(c ui.C) {
	if m.update.checkKey.Generation != 0 {
		ui.Effect(c, m.update.checkEffect, m.update.checkKey)
		if result, err, ok := m.update.checkEffect.Take(); ok {
			m.finishUpdateCheck(result, err)
		}
	}
	if m.update.downloadKey.Generation != 0 {
		ui.Effect(c, m.update.downloadEffect, m.update.downloadKey)
		if result, err, ok := m.update.downloadEffect.Take(); ok {
			m.finishUpdateDownload(result, err)
		}
	}
}

// showOrCheckUpdates opens the update that is already being prepared, or
// starts a manual check when there is nothing to show yet.
func (m *Model) showOrCheckUpdates() {
	if m.update.available || m.update.downloading || m.update.ready || m.update.errorKey != "" {
		m.update.open = true
		return
	}
	m.startUpdateCheck(true)
}

// startUpdateCheck begins one signed-feed check. Automatic checks stay silent
// when the network or the feed is unavailable; manual checks explain failure.
func (m *Model) startUpdateCheck(manual bool) {
	if !m.update.supported {
		if manual {
			m.update.open = true
			m.update.notesURL = githubReleasesURL
			m.update.errorKey = "updates.notInstalled"
		}
		return
	}
	if m.update.client == nil {
		if manual {
			m.update.open = true
			m.update.notesURL = githubReleasesURL
			m.update.errorKey = "updates.notConfigured"
		}
		return
	}
	if m.update.checking || m.update.downloading {
		if manual {
			m.update.open = true
		}
		return
	}
	m.update.checking = true
	m.update.manual = manual
	m.update.available = false
	m.update.ready = false
	m.update.version = ""
	m.update.notesURL = ""
	m.update.artifact = ""
	m.update.errorKey = ""
	m.update.checkSeq++
	m.update.checkKey = updateCheckKey{Generation: m.update.checkSeq}
	if manual {
		m.update.open = true
	}
	m.sess.Invalidate()
}

func (m *Model) finishUpdateCheck(result updateCheckResult, err error) {
	manual := m.update.manual
	m.update.checking = false
	m.update.checkKey = updateCheckKey{}
	if err != nil {
		slog.Warn("checking for an update failed", "error", err)
		if manual {
			m.update.open = true
			m.update.errorKey = m.updateErrorKey(err)
		}
		return
	}
	if result.feed == nil {
		m.update.available = false
		if manual {
			m.update.open = false
			m.info(m.t("updates.upToDate"))
		}
		return
	}
	release := result.feed.Latest
	asset, err := result.feed.Asset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		slog.Warn("the update feed has no package for this platform", "error", err)
		if manual {
			m.update.open = true
			m.update.errorKey = m.updateErrorKey(err)
		}
		return
	}
	m.update.available = true
	m.update.version = release.Version
	m.update.notesURL = release.NotesURL
	if m.update.notesURL == "" {
		m.update.notesURL = githubReleasesURL
	}
	m.update.asset = asset
	m.update.errorKey = ""
	m.update.open = true
	m.startUpdateDownload()
}

// startUpdateDownload downloads the verified archive automatically. Installing
// it still needs the user's explicit confirmation in the update dialog.
func (m *Model) startUpdateDownload() {
	if !m.update.available || m.update.downloading || m.update.ready || m.update.installing {
		return
	}
	directory, err := m.prepareUpdateDirectory()
	if err != nil {
		slog.Error("cannot prepare the update download directory", "error", err)
		m.update.open = true
		m.update.errorKey = "updates.errorGeneric"
		return
	}
	m.update.downloadSeq++
	m.update.downloadKey = updateDownloadKey{
		Generation: m.update.downloadSeq,
		Version:    m.update.version,
		Asset:      m.update.asset,
		Directory:  directory,
	}
	m.update.downloading = true
	m.update.errorKey = ""
	m.update.progressBits.Store(0)
	m.update.lastProgressWake.Store(0)
	m.update.open = true
	m.sess.Invalidate()
}

func (m *Model) finishUpdateDownload(result updateDownloadResult, err error) {
	m.update.downloading = false
	m.update.downloadKey = updateDownloadKey{}
	if err != nil {
		slog.Warn("downloading an update failed", "error", err)
		m.update.open = true
		m.update.errorKey = m.updateErrorKey(err)
		return
	}
	m.update.ready = true
	m.update.artifact = result.artifact
	m.update.version = result.version
	m.update.asset = result.asset
	m.update.progressBits.Store(math.Float32bits(1))
	if !m.update.open {
		m.info(m.tf("updates.readyToast", "version", result.version))
	}
	m.update.open = true
}

func (m *Model) prepareUpdateDirectory() (string, error) {
	directory := filepath.Join(filepath.Dir(m.sess.Config().Path()), "updates")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(directory, 0o700)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(directory, entry.Name())); err != nil {
			return "", err
		}
	}
	return directory, nil
}

func (m *Model) updateProgress(done, total int64) {
	fraction := float32(0)
	if total > 0 {
		fraction = float32(done) / float32(total)
		if fraction < 0 {
			fraction = 0
		}
		if fraction > 1 {
			fraction = 1
		}
	}
	m.update.progressBits.Store(math.Float32bits(fraction))
	now := time.Now().UnixNano()
	last := m.update.lastProgressWake.Load()
	if done < total && now-last < int64(100*time.Millisecond) {
		return
	}
	if m.update.lastProgressWake.CompareAndSwap(last, now) {
		m.sess.Invalidate()
	}
}

func (m *Model) progress() float32 {
	return math.Float32frombits(m.update.progressBits.Load())
}

func (m *Model) progressPercent() int {
	return int(m.progress()*100 + 0.5)
}

func (m *Model) updateBadgeLabel() string {
	switch {
	case m.update.ready:
		return m.t("updates.readyBadge")
	case m.update.downloading:
		return m.t("updates.downloadingBadge")
	case m.update.available:
		return m.t("updates.availableBadge")
	default:
		return ""
	}
}

func (m *Model) updateDialog(c ui.C) {
	state := &m.update
	ui.ModalWith(c, &state.open, m.t("updates.title"), ui.ModalSpec{Width: 560}, func() {
		switch {
		case state.installing:
			ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, m.t("updates.installing")).Bold()
			})
		case state.ready:
			ui.Text(c, m.tf("updates.ready", "version", state.version)).Bold()
			ui.Progress(c, 1)
		case state.downloading:
			ui.Text(c, m.tf("updates.downloading", "version", state.version)).Bold()
			ui.Progress(c, m.progress())
			ui.Text(c, m.tf("updates.percent", "percent", m.progressPercent())).Muted().FontSize(12)
		case state.checking:
			ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, m.t("updates.checking")).Bold()
			})
		case state.available:
			ui.Text(c, m.tf("updates.available", "version", state.version, "current", m.sess.Version())).Bold()
		default:
			ui.Text(c, m.t("updates.intro")).Muted()
		}
		if state.errorKey != "" {
			ui.Divider(c)
			ui.Text(c, m.t(state.errorKey)).Color(m.theme.Danger).FontSize(12)
		}
	}, func() {
		if state.notesURL != "" {
			if ui.Button(c, m.t("updates.notes")).Outline().Clicked() {
				after(c, m.openUpdateNotes)
			}
		}
		switch {
		case state.installing:
		case state.ready:
			if ui.Button(c, m.t("updates.later")).Outline().Clicked() {
				state.open = false
			}
			if ui.Button(c, m.t("updates.install")).Clicked() {
				after(c, m.installUpdate)
			}
		case state.downloading, state.checking:
			if ui.Button(c, m.t("updates.later")).Outline().Clicked() {
				state.open = false
			}
		default:
			retry := state.errorKey != "" &&
				state.errorKey != "updates.notInstalled" &&
				state.errorKey != "updates.notConfigured"
			if retry {
				if ui.Button(c, m.t("updates.retry")).Clicked() {
					if state.available {
						after(c, m.startUpdateDownload)
					} else {
						after(c, func() { m.startUpdateCheck(true) })
					}
				}
			}
			if ui.Button(c, m.t("updates.close")).Outline().Clicked() {
				state.open = false
			}
		}
	})
}

func (m *Model) openUpdateNotes() {
	target := m.update.notesURL
	if target == "" {
		target = githubReleasesURL
	}
	if err := app.OpenURL(target); err != nil {
		m.fail(err)
	}
}

func (m *Model) installUpdate() {
	state := &m.update
	if !state.ready || state.installing || state.artifact == "" || state.version == "" {
		return
	}
	if v := m.vault(); v != nil && v.Modified() {
		if err := m.sess.Save(); err != nil {
			m.fail(err)
			return
		}
	}
	state.installing = true
	err := state.installer(update.InstallOptions{
		Artifact: state.artifact,
		Asset:    state.asset,
		Version:  state.version,
		App:      updateAppID,
		Args:     os.Args[1:],
	}, func() {
		if m.sess.Config().Get().ClipboardClearOnExit {
			m.sess.ClearClipboard()
		}
		m.sess.SetQuitting()
		if m.application != nil {
			m.application.Quit()
		}
	})
	if err != nil {
		slog.Error("installing the update failed", "error", err)
		state.installing = false
		state.open = true
		state.errorKey = m.updateErrorKey(err)
		m.sess.Invalidate()
	}
}

func (m *Model) updateErrorKey(err error) string {
	switch {
	case errors.Is(err, update.ErrManagedExternally):
		return "updates.errorManaged"
	case errors.Is(err, update.ErrNotWritable):
		return "updates.errorNotWritable"
	case errors.Is(err, update.ErrUnsupported):
		return "updates.errorUnsupported"
	case errors.Is(err, update.ErrSignature),
		errors.Is(err, update.ErrChecksum),
		errors.Is(err, update.ErrSize),
		errors.Is(err, update.ErrUntrusted),
		errors.Is(err, update.ErrInsecureURL):
		return "updates.errorVerification"
	case errors.Is(err, update.ErrNoAsset):
		return "updates.errorNoAsset"
	case errors.Is(err, update.ErrExpired):
		return "updates.errorExpired"
	case errors.Is(err, update.ErrManifest),
		errors.Is(err, update.ErrTooLarge),
		errors.Is(err, update.ErrUnsafeArchive):
		return "updates.errorVerification"
	default:
		return "updates.errorGeneric"
	}
}
