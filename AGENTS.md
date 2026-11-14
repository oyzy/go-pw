# GoKeep

A password manager in the spirit of KeePass 2, with its own `.gkdb` file format: Go 1.27 +
Viva (the page is drawn on the GPU, there is no web view and no frontend build). Read this
before changing the UI.

```powershell
viva dev .         # run with hot reload: save a file, the window reloads and keeps its state
viva vet           # the page mistakes that compile but do the wrong thing
viva build         # a release program in bin/, with the icon and version resources
go test ./...      # tests drive the real application with no window
node tools/i18n-check.mjs   # every language pack has the same keys as zh-CN
```

The Viva guides ship inside the module, in
`$(go env GOMODCACHE)/github.com/vivaui/viva@v0.6.1/website/content/guides/zh/`, with runnable
programs in `examples/guides/`. Read the page guide before writing a new screen.

## How a window works here

`internal/desktop.Run` builds the page and opens the window. The page is one function,
`Model.page(c ui.C)` in `page.go`; Viva paints it again after every event that could change
the picture. It draws the whole window from `Model`, so it must be cheap and must not change
state, except that a click is handled where its control is declared.

Nothing draws unless an event, `Invalidate`, or an animation asks for a frame; there is no
frame loop. State lives in `Model` (`model.go`) and the `*State` structs in `state.go`. Never
keep a `ui.Box`, a `ui.C`, or a closure given to `Children` after the page returns.

Things that only this project has, which are easy to get wrong:

- **The tree and list callbacks get no context.** `ui.Tree` takes
  `label func(int) string` and `content func(int)`, and `ui.List` takes
  `func(ui.ListRow)`, so rows cannot declare themselves with a `ui.C`. `Model` caches the
  page context of the paint in progress (`pageC`, set on the first line of `page`) and the
  row closures (`treeName`, `treeContent`, `listContent`, made once in `newModel`).
- **`ui.ContextMenu(c, target, ...)` needs a target with an identity**: a control, a
  container with `Hit`, or a `LabelFor` element. A plain `ui.Text` is not one, which is why
  `treeRow` gives its label `.Hit(ui.ID(c), name)`.
- **`MenuItem.Keys` only prints the shortcut.** The action is declared separately in
  `shortcuts`, once per paint. The help dialog's list is built from `shortcutRows`, so keep
  the two in step.
- **The window has no `SetTitle`.** The title bar is drawn by `titlebar`, together with
  `windowControls`; `refreshWindowTitle` exists only so the call sites read the same as
  before.
- **Viva paints the window buttons itself** on Windows when `WindowCustomTitlebar` is set, so
  the title bar keeps clear of them with `app.TitlebarInsets()`.

## The database

There is exactly one file format: `internal/gkdb` writes the `.gkdb` container (a `GKDB`
magic, a header that is authenticated as associated data, and a JSON document sealed with
AES-256-GCM or ChaCha20-Poly1305). `internal/dbdoc` is the document model it carries and
the key derivation both layers share; `internal/vault` is the open session on top. A file
that is not a GoKeep container is refused as "not a database file", so a `.kdbx` written
by another application never reaches the parser. Behaviour the user asked for and that
must not regress:

- a new database has no default group (only the root line, which is not a group);
- deleting an entry or a group moves it to the recycle bin, which shows as **回收站** and can
  be hidden with `ShowRecycleBin`; `Shift+Delete` deletes for good;
- a wrong password is a localised sentence, never `[object Object]`;
- switching the theme and switching back works, and the title bar follows the theme;
- the Help menu holds no KeePass links.

## Testing

`app.NewDriver(page.Config(config()), nil)` runs the real application with no window:
`driver.Click(x, y)`, `Type`, `Key`, `Move`, `Scroll`, `Resize`, then `driver.Snapshot()` for
the labels, hits and rectangles painted. The driver only repaints when something asks it to,
so after changing the model directly call `Invalidate` before `Pump`. Test behaviour a user
sees (the row appeared, the count changed), not pixel positions: `app_test.go` is the model
to copy.

## Rules

- Allocation-conscious: no `fmt.Sprintf` per frame for text that has not changed; keep
  strings in the model.
- No reflection, no `map[string]any` for UI state, no goroutines started from a page
  function by hand.
- Every string the user reads comes from `internal/desktop/locales/*.json` through `m.t`.
  Adding a key means adding it to all five packs, in the same order.
