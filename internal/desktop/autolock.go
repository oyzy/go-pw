package desktop

import (
	"log/slog"

	"github.com/vivaui/viva/gfx"
)

const (
	autoLockStep       = 30
	autoLockMaxSeconds = 24 * 60 * 60
)

func clampAutoLockSeconds(seconds int) int {
	return min(max(seconds, 0), autoLockMaxSeconds)
}

// The track functions wrap the handlers the page configured. They run before
// the page routes the event, so typing, shortcuts, clicks and wheel scrolling
// all count even when a control handles the event and never reaches a fallback
// callback.

func (m *Model) trackKey(inner func(gfx.KeyEvent) gfx.EventResult) func(gfx.KeyEvent) gfx.EventResult {
	return func(event gfx.KeyEvent) gfx.EventResult {
		m.sess.TouchActivity()
		if inner == nil {
			return gfx.EventResult{}
		}
		return inner(event)
	}
}

func (m *Model) trackPointer(inner func(gfx.PointerEvent) gfx.EventResult) func(gfx.PointerEvent) gfx.EventResult {
	return func(event gfx.PointerEvent) gfx.EventResult {
		if event.Kind != gfx.PointerExit {
			m.sess.TouchActivity()
		}
		if inner == nil {
			return gfx.EventResult{}
		}
		return inner(event)
	}
}

func (m *Model) trackShortcut(inner func(gfx.KeyEvent) gfx.EventResult) func(gfx.KeyEvent) gfx.EventResult {
	return func(event gfx.KeyEvent) gfx.EventResult {
		m.sess.TouchActivity()
		if inner == nil {
			return gfx.EventResult{}
		}
		return inner(event)
	}
}

func (m *Model) trackScroll(inner func(gfx.ScrollEvent) gfx.EventResult) func(gfx.ScrollEvent) gfx.EventResult {
	return func(event gfx.ScrollEvent) gfx.EventResult {
		m.sess.TouchActivity()
		if inner == nil {
			return gfx.EventResult{}
		}
		return inner(event)
	}
}

func (m *Model) windowActivate() {
	m.sess.TouchActivity()
}

// windowMinimize locks the workspace on the way to the taskbar when the user
// asked for it, and treats a restore like any other activity.
func (m *Model) windowMinimize(minimized bool) {
	if !minimized {
		m.sess.TouchActivity()
		return
	}
	if !m.sess.Config().Get().LockOnMinimize {
		return
	}
	if m.lockNow(m.t("toast.lockedOnMinimize")) {
		slog.Info("workspace locked because the window was minimized")
	}
}

// wake is the window's OnWake callback. The idle timer only sets a request;
// this runs on the window thread, saves pending changes and clears the model
// before the locked page is painted.
func (m *Model) wake() {
	if !m.sess.TakeAutoLockRequest() || m.sess.Quitting() {
		return
	}
	if m.lockNow(m.t("toast.autoLocked")) {
		slog.Info("workspace locked after inactivity")
	}
}

// lockNow is the shared path for automatic locks. It saves unsaved changes
// first, and leaves the workspace open when that save fails so an inactivity
// timer can never discard the user's edits. It runs on the window thread.
func (m *Model) lockNow(message string) bool {
	v := m.sess.Vault()
	if v == nil {
		return false
	}
	if v.Modified() {
		if err := m.sess.Save(); err != nil {
			m.fail(err)
			m.sess.TouchActivity()
			return false
		}
	}
	m.sess.Lock()
	m.afterClose()
	if message != "" {
		m.info(message)
	}
	return true
}
