// GoKeep frontend: KeePass style main window, entry editor and dialogs.

import { Events, Window } from "/wails/runtime.js";
import { application, describeError, generator, onError, vault } from "./api.js";
import { SUPPORTED, applyTranslations, detectLanguage, formatDate, languageLabel, setLanguage, t } from "./i18n.js";
import { clear, confirmDialog, el, openModal, prompt, qs, qualityBadge, toast } from "./ui.js";

// qualityText turns the numeric score into a localised description. The backend reports
// its own English label, but the user interface uses the language pack instead.
function qualityText(score) {
    if (score >= 80) return t("quality.excellent");
    if (score >= 60) return t("quality.good");
    if (score >= 40) return t("quality.fair");
    if (score >= 20) return t("quality.weak");
    return t("quality.veryWeak");
}

onError((message, kind, options) => toast(message, kind, options));

// Never fail silently: an exception inside a click handler would otherwise look like
// "the button does nothing".
window.addEventListener("error", (event) => {
    toast(event.message, "error", { title: t("detail.internalError"), timeout: 10000 });
});
window.addEventListener("unhandledrejection", (event) => {
    toast(describeError(event.reason), "error", { title: t("detail.actionFailed"), timeout: 10000 });
});

const state = {
    info: null,
    tree: [],
    groups: [],
    entries: [],
    selectedGroupId: "",
    selectedEntryId: "",
    detail: null,
    search: "",
    searchOptions: { caseSensitive: false, regex: false, allGroups: false, includeProtected: false },
    showRecycleBin: true,
    sortKey: "title",
    sortAsc: true,
    config: null,
    iconNames: {},
    presets: {},
    clipboardTimer: null,
    recentFiles: [],
};

const treeNode = qs("#tree");
const entriesNode = qs("#entries");
const detailNode = qs("#detail");
const lockScreen = qs("#lock-screen");
const menubarNode = qs("#menubar");

// MENU_LAYOUT describes the application menu. The native menu bar is disabled (see
// WindowsWindow.DisableMenu) so the menu is drawn by the frontend and follows the theme
// and the language pack.
const MENU_LAYOUT = [
    {
        label: "menu.file", items: [
            { cmd: "file:new", label: "menu.newDatabase", accel: "Ctrl+N" },
            { cmd: "file:open", label: "menu.openDatabase", accel: "Ctrl+O" },
            { recent: true },
            { separator: true },
            { cmd: "file:save", label: "menu.save", accel: "Ctrl+S", vault: true },
            { cmd: "file:save-as", label: "menu.saveAs", accel: "Ctrl+Shift+S", vault: true },
            { cmd: "file:close", label: "menu.closeDatabase", accel: "Ctrl+W", vault: true },
            { separator: true },
            { cmd: "file:import-csv", label: "menu.importCsv", vault: true },
            { cmd: "file:export-csv", label: "menu.exportCsv", vault: true },
            { cmd: "file:backup", label: "menu.backup", vault: true },
            { separator: true },
            { cmd: "file:lock", label: "menu.lockWorkspace", accel: "Ctrl+L", vault: true },
            { cmd: "file:quit", label: "menu.exit" },
        ],
    },
    {
        label: "menu.edit", items: [
            { cmd: "edit:new-entry", label: "menu.newEntry", accel: "Ctrl+Shift+N", vault: true },
            { cmd: "edit:new-group", label: "menu.newGroup", accel: "Ctrl+Shift+G", vault: true },
            { separator: true },
            { cmd: "edit:duplicate", label: "menu.duplicateEntry", accel: "Ctrl+K", vault: true, entry: true },
            { cmd: "edit:delete", label: "menu.delete", accel: "Delete", vault: true, entry: true },
            { separator: true },
            { cmd: "edit:copy-user", label: "menu.copyUser", accel: "Ctrl+B", vault: true, entry: true },
            { cmd: "edit:copy-password", label: "menu.copyPassword", accel: "Ctrl+C", vault: true, entry: true },
            { cmd: "edit:copy-url", label: "menu.copyUrl", accel: "Ctrl+U", vault: true, entry: true },
            { cmd: "edit:open-url", label: "menu.openUrl", accel: "Ctrl+Shift+U", vault: true, entry: true },
            { separator: true },
            { cmd: "edit:find", label: "menu.find", accel: "Ctrl+F", vault: true },
            { cmd: "edit:find-all", label: "menu.findAll", accel: "Ctrl+Shift+F", vault: true },
        ],
    },
    {
        label: "menu.view", items: [
            { cmd: "view:refresh", label: "menu.refresh", accel: "F5" },
            { cmd: "view:recycle-bin", label: "menu.showRecycleBin", checked: () => state.showRecycleBin, vault: true },
            {
                cmd: "view:theme", label: "menu.toggleTheme",
                value: () => t(state.config.theme === "light" ? "options.themeLight" : "options.themeDark"),
            },
        ],
    },
    {
        label: "menu.tools", items: [
            { cmd: "tools:generator", label: "menu.generator", accel: "Ctrl+G", vault: true },
            { cmd: "tools:db-settings", label: "menu.dbSettings", vault: true },
            { cmd: "tools:change-key", label: "menu.changeKey", vault: true },
            { cmd: "tools:key-file", label: "menu.createKeyFile" },
            { separator: true },
            { cmd: "tools:duplicates", label: "menu.findDuplicates", vault: true },
            { cmd: "tools:expired", label: "menu.checkExpired", vault: true },
            { cmd: "tools:empty-bin", label: "menu.emptyBin", vault: true },
            { separator: true },
            { cmd: "tools:clear-clipboard", label: "menu.clearClipboard", accel: "Ctrl+Shift+C" },
            { cmd: "tools:options", label: "menu.options" },
        ],
    },
    {
        label: "menu.help", items: [
            { cmd: "help:about", label: "menu.about" },
        ],
    },
];

// ---------------------------------------------------------------- bootstrapping

async function boot() {
    state.config = await application.config();
    state.showRecycleBin = state.config.showRecycleBin !== false;
    // Language: the stored choice wins, otherwise the operating system decides.
    try {
        const initialLanguage = await setLanguage(state.config.language || detectLanguage(), { silent: true });
        state.config.language = initialLanguage;
        applyTranslations();
        applyShortcutHints();
    } catch (error) {
        // A broken language pack must never keep the user out of the application; the
        // markup already contains the default language as a fallback.
        console.warn("cannot load the language pack", error);
    }
    state.iconNames = await application.iconNames();
    state.presets = await application.presets();
    applyTheme(state.config.theme);
    qs("#status-version").textContent = `GoKeep ${await application.version()}`;
    setToolbarEnabled(false);
    await syncMenu();

    Events.On("menu", (event) => runCommand(event.data));
    Events.On("command", (event) => runCommand(event.data));

    qs("#search").addEventListener("input", (event) => {
        state.search = event.target.value;
        refreshEntries();
    });
    document.querySelectorAll("#toolbar [data-cmd]").forEach((button) => {
        button.addEventListener("click", () => runCommand(button.dataset.cmd));
    });
    qs("#edit-entry").addEventListener("click", () => {
        if (state.selectedEntryId) openEntryEditor(state.selectedEntryId);
    });
    // The status bar warning is the way back to a switched-off recycle bin.
    qs("#status-warning").addEventListener("click", () => {
        if (state.info && state.info.recycleBinEnabled === false) openDatabaseSettingsDialog();
    });
    document.querySelectorAll("table.entries thead th").forEach((th) => {
        th.addEventListener("click", () => {
            const key = th.dataset.sort;
            if (state.sortKey === key) {
                state.sortAsc = !state.sortAsc;
            } else {
                state.sortKey = key;
                state.sortAsc = true;
            }
            renderEntries();
        });
    });
    document.addEventListener("keydown", handleShortcut);
    document.addEventListener("click", () => closeMenus());
    document.addEventListener("keydown", (event) => {
        if (event.key === "Escape") {
            closeMenus();
        }
    });

    renderLock();
    renderMenuBar();
    setupTitleBar();
    setupContextMenus();
    startClipboardTicker();
    startAutoLock();
    if (state.config.lastDatabase) {
        qs("#lock-path").value = state.config.lastDatabase;
    }
}

// setupTitleBar wires the buttons of the custom title bar. The drag area itself is
// handled by Windows through the --wails-non-client-region CSS property.
function setupTitleBar() {
    const titlebar = qs("#titlebar");
    const maximizeButton = qs("#window-maximize");
    const setMaximised = (maximised) => {
        titlebar.classList.toggle("maximised", !!maximised);
        maximizeButton.setAttribute("title", t(maximised ? "titlebar.restore" : "titlebar.maximize"));
    };
    qs("#window-minimize").addEventListener("click", () => Window.Minimise());
    maximizeButton.addEventListener("click", () => Window.ToggleMaximise());
    // Closing asks about unsaved changes, exactly like the menu item.
    qs("#window-close").addEventListener("click", () => runCommand("file:quit"));
    Events.On("common:WindowMaximise", () => setMaximised(true));
    Events.On("common:WindowUnMaximise", () => setMaximised(false));
    Events.On("windows:WindowMaximise", () => setMaximised(true));
    Events.On("windows:WindowUnMaximise", () => setMaximised(false));
    Window.IsMaximised().then(setMaximised).catch(() => {});
    setMaximised(false);
}

// setWindowTitle updates the title shown in the custom title bar and the native title
// (which Windows uses for the taskbar button and the window list).
function setWindowTitle(text) {
    qs("#titlebar-title").textContent = text;
    application.setTitle(`${text} - GoKeep`);
}

// applyTheme switches the stylesheet theme and tells the backend so the native title bar
// follows it as well.
function applyTheme(theme) {
    const resolved = theme === "light" ? "light" : "dark";
    document.documentElement.dataset.theme = resolved;
    application.applyTheme(resolved).catch(() => {});
}

// applyShortcutHints appends the keyboard shortcut to the tooltip of every toolbar
// button, which keeps the accelerators out of the language packs.
function applyShortcutHints() {
    document.querySelectorAll("[data-shortcut]").forEach((node) => {
        const base = node.dataset.i18nTitle ? t(node.dataset.i18nTitle) : node.getAttribute("title") || "";
        node.setAttribute("title", `${base} (${node.dataset.shortcut})`);
    });
}

// renderMenuBar rebuilds the in-app menu bar from MENU_LAYOUT. It is re-rendered when the
// language changes and whenever a menu is opened, so enabled/checked states stay fresh.
function renderMenuBar() {
    const wasOpen = openMenuIndex();
    clear(menubarNode);
    MENU_LAYOUT.forEach((root, index) => {
        const container = el("div", { class: "menu-root" });
        const title = el("button", { class: "menu-title", text: t(root.label) });
        const dropdown = el("div", { class: "menu-dropdown" });
        title.addEventListener("click", (event) => {
            event.stopPropagation();
            toggleMenu(index);
        });
        title.addEventListener("mouseenter", () => {
            if (openMenuIndex() !== -1) {
                openMenu(index);
            }
        });
        container.append(title, dropdown);
        menubarNode.append(container);
    });
    if (wasOpen !== -1) {
        openMenu(wasOpen);
    }
}

function openMenuIndex() {
    const roots = [...menubarNode.querySelectorAll(".menu-root")];
    return roots.findIndex((node) => node.classList.contains("open"));
}

function closeMenus() {
    menubarNode.querySelectorAll(".menu-root.open").forEach((node) => node.classList.remove("open"));
}

function toggleMenu(index) {
    if (openMenuIndex() === index) {
        closeMenus();
        return;
    }
    openMenu(index);
}

function openMenu(index) {
    const roots = [...menubarNode.querySelectorAll(".menu-root")];
    roots.forEach((node, i) => node.classList.toggle("open", i === index));
    const dropdown = roots[index].querySelector(".menu-dropdown");
    clear(dropdown);
    buildMenuItems(dropdown, MENU_LAYOUT[index].items);
}

function buildMenuItems(container, items) {
    for (const item of items) {
        if (item.separator) {
            container.append(el("div", { class: "menu-separator" }));
            continue;
        }
        if (item.recent) {
            buildRecentItems(container);
            continue;
        }
        const disabled = (item.vault && !state.info) || (item.entry && !state.selectedEntryId);
        const check = item.checked && item.checked() ? "✓" : "";
        const value = item.value ? item.value() : "";
        const button = el("button", { class: "menu-item" }, [
            el("span", { class: "menu-check", text: check }),
            el("span", { class: "menu-label", text: t(item.label) }),
            el("span", { class: "menu-accel", text: item.accel || value }),
        ]);
        button.disabled = !!disabled;
        button.addEventListener("click", () => {
            closeMenus();
            runCommand(item.cmd);
        });
        container.append(button);
    }
}

function buildRecentItems(container) {
    const files = state.recentFiles || [];
    if (files.length === 0) {
        container.append(el("div", { class: "menu-empty", text: t("menu.recentNone") }));
        return;
    }
    for (const file of files.slice(0, 8)) {
        const button = el("button", { class: "menu-item" }, [
            el("span", { class: "menu-check", text: "" }),
            el("span", { class: "menu-label", text: shortenPath(file) }),
            el("span", { class: "menu-accel", text: "" }),
        ]);
        button.addEventListener("click", () => {
            closeMenus();
            runCommand(`file:recent:${file}`);
        });
        container.append(button);
    }
    container.append(el("div", { class: "menu-separator" }));
    const clearItem = el("button", { class: "menu-item" }, [
        el("span", { class: "menu-check", text: "" }),
        el("span", { class: "menu-label", text: t("menu.clearList") }),
        el("span", { class: "menu-accel", text: "" }),
    ]);
    clearItem.addEventListener("click", () => {
        closeMenus();
        runCommand("file:recent-clear");
    });
    container.append(clearItem);
}

function shortenPath(path) {
    return path.length <= 52 ? path : `…${path.slice(-49)}`;
}

// syncMenu tells the backend the current language (it stores it and uses it for the
// recycle bin group name) and refreshes the recent file list shown in the menu.
async function syncMenu() {
    const labels = { recycleBin: t("app.recycleBin") };
    try {
        await application.applyLanguage(state.config.language, labels);
    } catch (error) {
        console.warn("cannot store the language", error);
    }
    await refreshRecentFiles();
    renderMenuBar();
}

// refreshRecentFiles keeps the "open recent" part of the menu in sync.
async function refreshRecentFiles() {
    state.recentFiles = await application.recentFiles();
}

// applyLanguage switches the complete interface, including the native menu bar, to
// another language pack.
async function applyLanguage(code) {
    const language = await setLanguage(code);
    state.config.language = language;
    applyTranslations();
    applyShortcutHints();
    await syncMenu();
    renderMenuBar();
    if (state.info) {
        await refreshAll();
        if (state.selectedEntryId && state.detail) {
            state.detail = await vault.entry(state.selectedEntryId);
            renderDetail();
        }
    } else {
        renderLock();
    }
}

function setToolbarEnabled(enabled) {
    document.querySelectorAll("[data-requires-vault]").forEach((button) => {
        button.disabled = !enabled;
    });
    qs("#search").disabled = !enabled;
}

// ---------------------------------------------------------------- lock screen

function renderLock() {
    clear(lockScreen);
    if (state.info) {
        lockScreen.style.display = "none";
        return;
    }
    lockScreen.classList.add("lock");
    lockScreen.style.display = "flex";
    state.entries = [];
    state.tree = [];
    state.selectedEntryId = "";
    state.detail = null;
    renderTree();
    renderEntries();
    renderDetail();
    renderStatus();

    const pathInput = el("input", { attrs: { type: "text", id: "lock-path", placeholder: t("lock.pathPlaceholder") } });
    const passwordInput = el("input", { attrs: { type: "password", id: "lock-password", placeholder: t("create.password") } });
    const keyInput = el("input", { attrs: { type: "text", id: "lock-keyfile", placeholder: t("lock.keyFilePlaceholder") } });
    const browseKey = el("button", { class: "btn", text: t("lock.chooseKeyFile") });
    const useKeyFile = el("input", { attrs: { type: "checkbox" } });
    const keyRow = el("div", { class: "row" }, [keyInput, browseKey]);
    const openButton = el("button", { class: "btn primary", text: t("lock.unlock") });
    const browse = el("button", { class: "btn", text: t("lock.browse") });
    const create = el("button", { class: "btn", text: t("create.defaultName") });
    const errorBox = el("div", { class: "lock-error" });
    const recentBox = el("div", { class: "list-box", style: { marginTop: "14px" } });

    const fillRecent = async () => {
        clear(recentBox);
        const files = await application.recentFiles();
        if (files.length === 0) {
            recentBox.append(el("div", { class: "item hint", text: t("lock.recentEmpty") }));
            return;
        }
        for (const file of files) {
            const open = el("button", { class: "btn ghost", text: t("expired.open") });
            open.addEventListener("click", () => {
                pathInput.value = file;
                passwordInput.focus();
            });
            recentBox.append(el("div", { class: "item" }, [
                el("span", { class: "grow mono", text: file }), open,
            ]));
        }
    };

    browse.addEventListener("click", async () => {
        const path = await application.openFileDialog(t("fileDialog.openDatabase"), "*.kdbx;*.gkdb", "");
        if (path) pathInput.value = path;
    });
    browseKey.addEventListener("click", async () => {
        const path = await application.openFileDialog(t("lock.chooseKeyFile"), "*.keyx;*.key;*", "");
        if (path) {
            keyInput.value = path;
            useKeyFile.checked = true;
            keyRow.style.display = "";
        }
    });
    useKeyFile.addEventListener("change", () => {
        keyRow.style.display = useKeyFile.checked ? "" : "none";
        if (!useKeyFile.checked) keyInput.value = "";
    });
    keyRow.style.display = "none";
    const doOpen = async () => {
        const path = pathInput.value.trim();
        errorBox.classList.remove("show");
        errorBox.textContent = "";
        passwordInput.classList.remove("invalid");
        if (!path) {
            showLockError(errorBox, passwordInput, "", t("lock.chooseFileFirst"));
            return;
        }
        const keyFilePath = useKeyFile.checked ? keyInput.value.trim() : "";
        if (useKeyFile.checked && !keyFilePath) {
            showLockError(errorBox, passwordInput, "", t("lock.chooseKeyFileFirst"));
            return;
        }
        await openDatabase(path, passwordInput.value, keyFilePath, (error) => {
            showLockError(errorBox, passwordInput, error.code, error.message || describeError(error));
        });
    };
    pathInput.addEventListener("input", () => errorBox.classList.remove("show"));
    passwordInput.addEventListener("input", () => {
        errorBox.classList.remove("show");
        passwordInput.classList.remove("invalid");
    });
    openButton.addEventListener("click", doOpen);
    passwordInput.addEventListener("keydown", (event) => {
        if (event.key === "Enter") doOpen();
    });
    create.addEventListener("click", openNewDatabaseDialog);

    lockScreen.append(el("div", { class: "lock-card" }, [
        el("img", { attrs: { src: "/icon.png", alt: "GoKeep", width: "56", height: "56" },
            style: { display: "block", marginBottom: "10px" } }),
        el("h1", { text: "GoKeep" }),
        el("p", { text: t("app.tagline") }),
        el("label", { text: t("lock.file") }),
        el("div", { class: "row" }, [pathInput, browse]),
        el("label", { text: t("create.password") }),
        passwordInput,
        el("label", { class: "checkbox" }, [useKeyFile, t("lock.useKeyFile")]),
        keyRow,
        el("div", { class: "lock-actions" }, [openButton, create]),
        errorBox,
        recentBox,
    ]));
    fillRecent();
    setTimeout(() => pathInput.focus({ preventScroll: true }), 40);
}

// showMain reveals the workspace after a database was opened or created.
function showMain() {
    lockScreen.style.display = "none";
    setToolbarEnabled(true);
    const name = (state.info && (state.info.name || state.info.path)) || "GoKeep";
    setWindowTitle(name);
}

// showLockError puts a failure right under the unlock button, where the user is looking,
// instead of only in the corner of the window.
function showLockError(box, passwordInput, code, message) {
    box.textContent = message;
    box.classList.add("show");
    if (code === "wrong-key" || code === "wrong-key-file" || code === "") {
        passwordInput.classList.add("invalid");
        passwordInput.focus({ preventScroll: true });
        passwordInput.select();
    }
}

async function openDatabase(path, password, keyFile, onError) {
    try {
        state.info = await vault.open(path, password, keyFile);
        toast(t("toast.opened", { name: state.info.name }), "success");
        await refreshAll();
        await refreshRecentFiles();
        showMain();
        return true;
    } catch (error) {
        state.info = null;
        if (onError) {
            // The unlock card is still on screen, so show the failure inline there; the
            // toast has already been raised by the API layer.
            onError(error);
            return false;
        }
        renderLock();
        return false;
    }
}

function openNewDatabaseDialog() {
    const pathInput = el("input", { attrs: { type: "text" } });
    const nameInput = el("input", { attrs: { type: "text", value: "New Database" } });
    const passwordInput = el("input", { attrs: { type: "password" } });
    const repeatInput = el("input", { attrs: { type: "password" } });
    const keyInput = el("input", { attrs: { type: "text", placeholder: t("changeKey.optional") } });
    const strength = el("div", { class: "hint" });
    const presetSelect = el("select");
    const formatSelect = el("select");
    formatSelect.append(el("option", { attrs: { value: "kdbx" }, text: t("create.formatKdbx") }));
    formatSelect.append(el("option", { attrs: { value: "gokeep" }, text: t("create.formatGoKeep") }));
    const formatHint = el("div", { class: "hint" });
    const browse = el("button", { class: "btn", text: t("lock.browse") });
    const browseKey = el("button", { class: "btn", text: t("changeKey.choose") });
    const createKey = el("button", { class: "btn", text: t("create.generateKeyFile") });

    browse.addEventListener("click", async () => {
        const native = formatSelect.value === "gokeep";
        const suffix = native ? ".gkdb" : ".kdbx";
        const path = await application.saveFileDialog(t("create.defaultName"), `Database${suffix}`, `*${suffix}`);
        if (path) pathInput.value = path;
    });
    // The chosen format decides the file name suffix and the explanation below it.
    const syncFormat = () => {
        const native = formatSelect.value === "gokeep";
        formatHint.textContent = t(native ? "create.formatHintGoKeep" : "create.formatHintKdbx");
        // Only the presets that apply to the chosen format are offered.
        const previous = presetSelect.value;
        clear(presetSelect);
        for (const [key] of presetsForFormat(formatSelect.value)) {
            presetSelect.append(el("option", { attrs: { value: key }, text: presetLabel(key) }));
        }
        if ([...presetSelect.options].some((option) => option.value === previous)) {
            presetSelect.value = previous;
        }
        if (pathInput.value) {
            pathInput.value = pathInput.value.replace(/\.(kdbx|gkdb)$/i, native ? ".gkdb" : ".kdbx");
        }
    };
    formatSelect.addEventListener("change", syncFormat);
    syncFormat();
    browseKey.addEventListener("click", async () => {
        const path = await application.openFileDialog(t("lock.chooseKeyFile"), "*.keyx;*", "");
        if (path) keyInput.value = path;
    });
    createKey.addEventListener("click", async () => {
        const path = await application.saveFileDialog(t("fileDialog.saveKeyFile"), "Database.keyx", "*.keyx");
        if (!path) return;
        try {
            await vault.createKeyFile(path);
            keyInput.value = path;
            toast(t("toast.keyFileCreatedPlain"), "success");
        } catch (error) {
            /* reported */
        }
    });
    passwordInput.addEventListener("input", async () => {
        const result = await vault.evaluate(passwordInput.value);
        const [, score] = result;
        strength.textContent = passwordInput.value ? t("create.strength", { label: qualityText(score), score: score }) : "";
    });

    const createButton = el("button", { class: "btn primary", text: t("group.createAction") });
    const cancelButton = el("button", { class: "btn", text: t("dialog.cancel") });
    const dialog = openModal({
        title: t("create.defaultName"),
        body: el("div", { class: "grid" }, [
            el("label", { text: t("menu.file") }),
            el("div", { class: "row" }, [pathInput, browse]),
            el("label", { text: t("settings.name") }), nameInput,
            el("label", { text: t("create.format") }), formatSelect,
            el("div", { class: "full" }, [formatHint]),
            el("label", { text: t("create.password") }), passwordInput,
            el("label", { text: t("changeKey.repeat") }), repeatInput,
            el("label", { text: t("changeKey.keyFile") }),
            el("div", { class: "row" }, [keyInput, browseKey, createKey]),
            el("label", { text: t("create.preset") }), presetSelect,
            el("div", { class: "full" }, [strength]),
        ]),
        footer: [cancelButton, createButton],
    });
    cancelButton.addEventListener("click", () => dialog.close());
    createButton.addEventListener("click", async () => {
        if (passwordInput.value !== repeatInput.value) {
            toast(t("changeKey.mismatch"), "error");
            return;
        }
        if (!passwordInput.value && !keyInput.value.trim()) {
            toast(t("create.needKey"), "error");
            return;
        }
        const settings = Object.assign({}, state.presets[presetSelect.value] || {}, {
            name: nameInput.value,
            format: formatSelect.value,
            historyMaxItems: 10,
            historyMaxSize: 6 * 1024 * 1024,
            protectPassword: true,
        });
        try {
            state.info = await vault.create(pathInput.value.trim(), nameInput.value, passwordInput.value, keyInput.value.trim(), settings);
            dialog.close();
            await refreshAll();
            showMain();
            toast(t("toast.created"), "success");
        } catch (error) {
            /* reported */
        }
    });
}

function presetLabel(key) {
    switch (key) {
        case "paranoid": return t("create.presetParanoid");
        case "legacy-3.1": return t("create.presetLegacy");
        case "gokeep-defaults": return t("create.presetGoKeep");
        default: return t("create.presetDefault");
    }
}

// presetsForFormat lists the security presets that make sense for a file format: the
// "KDBX 3.1" preset is meaningless for the native format, and the native formats have
// their own default entry.
function presetsForFormat(format) {
    return Object.entries(state.presets).filter(([, preset]) => {
        const presetFormat = preset.format || "";
        return presetFormat === "" || presetFormat === format;
    });
}

// ---------------------------------------------------------------- data loading

async function refreshAll() {
    if (!state.info) return;
    state.info = await vault.info();
    state.tree = await vault.tree();
    state.groups = await vault.groups(true);
    if (!state.selectedGroupId) {
        state.selectedGroupId = await vault.rootId();
    }
    await refreshEntries();
    renderTree();
    renderStatus();
}

async function refreshEntries() {
    if (!state.info) return;
    if (state.search.trim()) {
        state.entries = await vault.search({
            query: state.search,
            groupId: state.selectedGroupId,
            searchInAllGroups: state.searchOptions.allGroups,
            caseSensitive: state.searchOptions.caseSensitive,
            regex: state.searchOptions.regex,
            fields: [],
            // Entries inside the recycle bin are only listed when the bin itself is
            // selected or searched, which the backend decides.
            includeRecycled: false,
            includeProtected: state.searchOptions.includeProtected,
        });
    } else {
        state.entries = await vault.entries(state.selectedGroupId, true, false);
    }
    if (state.selectedEntryId && !state.entries.some((e) => e.id === state.selectedEntryId)) {
        state.selectedEntryId = "";
        state.detail = null;
        renderDetail();
    }
    renderEntries();
    renderStatus();
}

// inRecycleBin reports whether the currently selected group is the recycle bin.
function inRecycleBin(groupId) {
    return !!(state.info && groupId && state.info.recycleBinId &&
        groupId.toLowerCase() === state.info.recycleBinId.toLowerCase());
}

// setRecycleBinVisible shows or hides the recycle bin in the tree and remembers the choice.
// Deleting an entry calls it with true, so an entry that was just moved to the bin can never
// disappear into a hidden group.
async function setRecycleBinVisible(visible) {
    state.showRecycleBin = visible;
    const config = await application.config();
    config.showRecycleBin = visible;
    await application.updateConfig(config);
    state.config = config;
    if (!visible && inRecycleBin(state.selectedGroupId)) {
        await selectGroup(await vault.rootId());
    } else {
        renderTree();
    }
}

// ---------------------------------------------------------------- rendering

function renderTree() {
    clear(treeNode);
    if (!state.info) {
        treeNode.append(el("div", { class: "hint", text: t("status.notOpen") }));
        return;
    }
    const rootRow = el("div", { class: "tree-row" + (state.selectedGroupId === state.rootId ? " selected" : "") }, [
        el("span", { class: "twist", text: "" }),
        el("span", { text: t("tree.allEntries") }),
    ]);
    rootRow.addEventListener("click", () => selectGroup(state.rootId));
    treeNode.append(rootRow);
    for (const group of state.tree) {
        treeNode.append(renderGroupNode(group, 0));
    }
}

function renderGroupNode(group, depth) {
    // The recycle bin is a system group: it is shown as a dimmed node with a bin glyph
    // and can be hidden from the view menu, exactly like KeePass' "Show recycle bin".
    if (inRecycleBin(group.id) && !state.showRecycleBin) {
        return el("div");
    }
    const container = el("div", { class: "tree-group" });
    const twist = el("span", { class: "twist", text: group.children && group.children.length ? (group.isExpanded ? "▾" : "▸") : "" });
    const row = el("div", {
        class: "tree-row" +
            (state.selectedGroupId === group.id ? " selected" : "") +
            (inRecycleBin(group.id) ? " system-group" : ""),
        style: { paddingLeft: `${6 + depth * 12}px` },
    }, [
        twist,
        el("span", { text: inRecycleBin(group.id) ? `🗑 ${group.name}` : (group.name || t("tree.unnamed")) }),
        el("span", { class: "count", text: String((group.entryIds || []).length) }),
    ]);
    const children = el("div", { class: "tree-children" + (group.isExpanded ? "" : " hidden") });
    for (const child of group.children || []) {
        children.append(renderGroupNode(child, depth + 1));
    }
    twist.addEventListener("click", async (event) => {
        event.stopPropagation();
        group.isExpanded = !group.isExpanded;
        children.classList.toggle("hidden", !group.isExpanded);
        twist.textContent = group.isExpanded ? "▾" : "▸";
        try {
            await vault.updateGroup(Object.assign({}, group, { isExpanded: group.isExpanded }));
        } catch (error) {
            /* reported */
        }
    });
    row.addEventListener("click", () => selectGroup(group.id));
    row.addEventListener("dblclick", () => {
        if (!inRecycleBin(group.id)) openGroupDialog(group);
    });
    row.addEventListener("contextmenu", (event) => {
        event.preventDefault();
        showGroupContextMenu(event, group);
    });
    container.append(row, children);
    return container;
}

async function selectGroup(groupId) {
    state.selectedGroupId = groupId;
    state.search = "";
    qs("#search").value = "";
    renderTree();
    await refreshEntries();
    const group = state.groups.find((g) => g.id === groupId);
    qs("#list-title").textContent = group ? group.name : t("tree.allEntries");
}

function showGroupContextMenu(event, group) {
    if (inRecycleBin(group.id)) {
        showContextMenu(event, [[t("menu.emptyBin"), () => runCommand("tools:empty-bin")]]);
        return;
    }
    const items = [
        [t("group.menuNewEntry"), () => openEntryEditor(null, group.id)],
        [t("group.menuNewGroup"), () => createGroupDialog(group.id)],
        [t("group.menuEdit"), () => openGroupDialog(group)],
        [t("group.menuMove"), () => moveDialog(group.id, true)],
        [t("confirm.deleteGroupTitle"), () => deleteGroup(group)],
        [t("confirm.deleteForever"), () => deleteGroup(group, true)],
    ];
    showContextMenu(event, items);
}

function showContextMenu(event, items) {
    const menu = el("div", {
        class: "modal",
        style: {
            position: "fixed", left: `${event.clientX}px`, top: `${event.clientY}px`,
            width: "200px", padding: "6px", "z-index": "250",
        },
    });
    for (const [label, handler] of items) {
        const item = el("div", { class: "tree-row", text: label });
        item.addEventListener("click", () => {
            menu.remove();
            handler();
        });
        menu.append(item);
    }
    document.body.append(menu);
    const close = (e) => {
        if (!menu.contains(e.target)) {
            menu.remove();
            document.removeEventListener("mousedown", close);
        }
    };
    setTimeout(() => document.addEventListener("mousedown", close), 10);
}

// setupContextMenus replaces the menu of the embedded browser. That menu offers "reload",
// "save as", "print" and "inspect", none of which belongs in a password manager, so only
// right-clicks that no other part of the interface claims get a menu of its own.
function setupContextMenus() {
    document.addEventListener("contextmenu", (event) => {
        // The group tree, the entry list and the dialogs bring their own menus.
        if (event.defaultPrevented) return;
        event.preventDefault();
        const items = contextMenuItemsFor(event.target);
        if (items.length) showContextMenu(event, items);
    });
}

// contextMenuItemsFor picks the actions that make sense for the place that was clicked.
function contextMenuItemsFor(target) {
    const field = target.closest("input, textarea");
    if (field && !field.disabled && !field.readOnly && field.type !== "checkbox" && field.type !== "radio") {
        return [
            [t("context.cut"), () => document.execCommand("cut")],
            [t("context.copy"), () => document.execCommand("copy")],
            [t("context.paste"), () => pasteInto(field)],
            [t("context.selectAll"), () => field.select()],
        ];
    }
    if (!state.info) return [];
    const items = [];
    if (target.closest(".pane-tree")) {
        items.push([t("menu.newGroup"), () => runCommand("edit:new-group")]);
        items.push([t("menu.newEntry"), () => runCommand("edit:new-entry")]);
        items.push([t("menu.refresh"), () => runCommand("view:refresh")]);
        items.push([
            state.showRecycleBin ? t("menu.hideRecycleBin") : t("menu.showRecycleBin"),
            () => runCommand("view:recycle-bin"),
        ]);
    } else if (target.closest(".pane-list")) {
        items.push([t("menu.newEntry"), () => runCommand("edit:new-entry")]);
        if (state.search.trim()) items.push([t("context.clearSearch"), () => clearSearch()]);
        items.push([t("menu.refresh"), () => runCommand("view:refresh")]);
    } else if (target.closest("#detail-pane")) {
        if (state.selectedEntryId) {
            items.push([t("group.menuEditEntry"), () => openEntryEditor(state.selectedEntryId)]);
            items.push([t("menu.copyPassword"), () => copyField(state.selectedEntryId, "Password")]);
            items.push([t("menu.copyUser"), () => copyField(state.selectedEntryId, "UserName")]);
        }
        items.push([t("menu.refresh"), () => runCommand("view:refresh")]);
    } else {
        return [];
    }
    if (recycleBinExists()) {
        items.push([t("menu.emptyBin"), () => runCommand("tools:empty-bin")]);
    }
    return items;
}

// recycleBinExists reports whether the open database already has a recycle bin group.
function recycleBinExists() {
    return !!state.info && state.groups.some((group) => inRecycleBin(group.id));
}

// pasteInto inserts the clipboard content at the caret of a text field. The browser only
// hands the clipboard to a page after a user gesture, which is exactly what a menu click is.
async function pasteInto(field) {
    try {
        const text = await navigator.clipboard.readText();
        if (text === "") return;
        const start = field.selectionStart === null ? field.value.length : field.selectionStart;
        const end = field.selectionEnd === null ? field.value.length : field.selectionEnd;
        field.setRangeText(text, start, end, "end");
        field.dispatchEvent(new Event("input", { bubbles: true }));
    } catch (error) {
        toast(t("context.pasteBlocked"), "warning");
    }
}

function clearSearch() {
    state.search = "";
    qs("#search").value = "";
    refreshEntries();
}

function sortEntries(list) {
    const key = state.sortKey;
    const factor = state.sortAsc ? 1 : -1;
    return list.slice().sort((a, b) => {
        const value = (entry) => {
            switch (key) {
                case "user": return entry.userName || "";
                case "url": return entry.url || "";
                case "quality": return entry.quality || 0;
                case "modified": return entry.modified || "";
                default: return entry.title || "";
            }
        };
        const left = value(a);
        const right = value(b);
        if (typeof left === "number" && typeof right === "number") {
            return (left - right) * factor;
        }
        return String(left).localeCompare(String(right), "zh-CN") * factor;
    });
}

function renderEntries() {
    clear(entriesNode);
    const list = sortEntries(state.entries);
    for (const entry of list) {
        const row = el("tr", { class: state.selectedEntryId === entry.id ? "selected" : "" });
        const title = el("td");
        title.append(el("span", { text: entry.title || t("detail.noTitle") }));
        if (entry.isExpired) title.append(el("span", { class: "badge danger", text: t("detail.expiredBadge") }));
        if (entry.hasTotp) title.append(el("span", { class: "badge", text: "TOTP" }));
        if (entry.attachments > 0) title.append(el("span", { class: "badge", text: t("detail.attachmentBadge", { count: entry.attachments }) }));
        if (entry.isRecycled) title.append(el("span", { class: "badge warn", text: t("detail.recycledBadge") }));
        row.append(title);
        row.append(el("td", { text: entry.userName || "" }));
        row.append(el("td", { text: entry.url || "" }));
        const quality = el("td");
        quality.append(qualityBadge(entry.quality || 0));
        row.append(quality);
        row.append(el("td", { text: formatDate(entry.modified) }));
        row.addEventListener("click", () => selectEntry(entry.id));
        row.addEventListener("dblclick", () => openEntryEditor(entry.id));
        row.addEventListener("contextmenu", (event) => {
            event.preventDefault();
            if (entry.isRecycled) {
                showContextMenu(event, [
                    [t("group.menuRestore"), () => restoreEntry(entry.id)],
                    [t("group.menuRestoreTo"), () => restoreToGroupDialog(entry.id)],
                    [t("confirm.deleteForever"), () => deleteEntry(entry)],
                ]);
                return;
            }
            showContextMenu(event, [
                [t("group.menuEditEntry"), () => openEntryEditor(entry.id)],
                [t("menu.copyPassword"), () => copyField(entry.id, "Password")],
                [t("menu.copyUser"), () => copyField(entry.id, "UserName")],
                [t("menu.copyUrl"), () => copyField(entry.id, "URL")],
                [t("menu.openUrl"), () => openUrl(entry.id)],
                [t("group.menuMove"), () => moveDialog(entry.id, false)],
                [t("menu.duplicateEntry"), () => duplicateEntry(entry.id)],
                [t("confirm.deleteTitle"), () => deleteEntry(entry)],
                [t("confirm.deleteForever"), () => deleteEntry(entry, true)],
            ]);
        });
        entriesNode.append(row);
    }
    qs("#list-count").textContent = t("list.count", { count: list.length });
    qs("#list-hint").textContent = inRecycleBin(state.selectedGroupId)
        ? t("list.recycleHint")
        : "";
}

async function selectEntry(id) {
    state.selectedEntryId = id;
    state.detail = await vault.entry(id);
    renderEntries();
    renderDetail();
    qs("#edit-entry").disabled = false;
}

function fieldRow(label, value, options = {}) {
    const row = el("div", { class: "field-row" });
    row.append(el("div", { class: "field-label", text: label }));
    const hidden = options.hidden;
    const valueNode = el("div", {
        class: "field-value" + (options.mono ? " mono" : ""),
        text: hidden ? "••••••••" : (value || ""),
    });
    row.append(valueNode);
    if (options.reveal) {
        const toggle = el("button", { class: "icon-btn", text: "👁", attrs: { title: t("detail.reveal") } });
        toggle.addEventListener("click", () => {
            const isHidden = valueNode.textContent === "••••••••";
            valueNode.textContent = isHidden ? (value || "") : "••••••••";
        });
        row.append(toggle);
    }
    if (options.copy !== false) {
        const copy = el("button", { class: "icon-btn", text: "⧉", attrs: { title: t("generator.copy") } });
        copy.addEventListener("click", () => copyField(state.selectedEntryId, options.field || label));
        row.append(copy);
    }
    return row;
}

function renderDetail() {
    clear(detailNode);
    const entry = state.detail;
    if (!entry || !state.info) {
        detailNode.append(el("div", { class: "detail-empty", text: t("detail.select") }));
        qs("#edit-entry").disabled = true;
        return;
    }
    detailNode.append(el("div", { class: "section-title", text: entry.groupPath || "" }));
    detailNode.append(fieldRow(t("field.title"), entry.title, { field: "Title", copy: false }));
    detailNode.append(fieldRow(t("field.userName"), entry.userName, { field: "UserName" }));
    detailNode.append(fieldRow(t("field.password"), entry.password, { field: "Password", hidden: true, reveal: true, mono: true }));
    detailNode.append(fieldRow(t("field.url"), entry.url, { field: "URL" }));
    if (entry.notes) {
        detailNode.append(el("div", { class: "section-title", text: t("group.notes") }));
        detailNode.append(el("div", { class: "mono", style: { whiteSpace: "pre-wrap" }, text: entry.notes }));
    }
    if (entry.tags && entry.tags.length) {
        const tags = el("div", { class: "row wrap", style: { marginTop: "8px" } });
        for (const tag of entry.tags) tags.append(el("span", { class: "badge", text: tag }));
        detailNode.append(tags);
    }
    if (entry.fields && entry.fields.length) {
        detailNode.append(el("div", { class: "section-title", text: t("editor.tabFields") }));
        for (const field of entry.fields) {
            detailNode.append(fieldRow(field.key, field.value, {
                field: field.key, hidden: field.protected, reveal: field.protected, mono: true,
            }));
        }
    }
    if (entry.attachments && entry.attachments.length) {
        detailNode.append(el("div", { class: "section-title", text: t("editor.tabAttachments") }));
        for (const attachment of entry.attachments) {
            const button = el("button", { class: "icon-btn", text: t("editor.exportAttachment") });
            button.addEventListener("click", () => exportAttachment(attachment.key));
            detailNode.append(el("div", { class: "field-row" }, [
                el("div", { class: "field-value", text: t("field.attachmentSize", { name: attachment.key, size: attachment.size }) }),
                button,
            ]));
        }
    }
    if (entry.history && entry.history.length) {
        detailNode.append(el("div", { class: "section-title", text: t("detail.historyItem", { count: entry.history.length }) }));
        entry.history.forEach((revision, index) => {
            const restore = el("button", { class: "icon-btn", text: t("detail.restore") });
            restore.addEventListener("click", async () => {
                if (!await confirmDialog({ title: t("detail.historyRestoreTitle"), message: t("detail.historyRestoreConfirm") })) return;
                await vault.restoreHistory(entry.id, index);
                await selectEntry(entry.id);
                await refreshEntries();
            });
            detailNode.append(el("div", { class: "field-row" }, [
                el("div", { class: "field-value", text: formatDate(revision.modified) }),
                restore,
            ]));
        });
    }
    const meta = el("div", { class: "section-title", text: t("detail.sectionTimes") });
    detailNode.append(meta);
    detailNode.append(el("div", { class: "hint", text: t("detail.createdLine", { date: formatDate(entry.created) }) }));
    detailNode.append(el("div", { class: "hint", text: t("detail.modifiedLine", { date: formatDate(entry.modified) }) }));
    detailNode.append(el("div", { class: "hint", text: t("detail.accessedLine", { date: formatDate(entry.accessed), count: entry.usageCount }) }));
    if (entry.expires) {
        detailNode.append(el("div", { class: entry.isExpired ? "badge danger" : "badge warn", text: t("detail.expiryLine", { date: formatDate(entry.expiryTime) }) }));
    }
    detailNode.append(el("div", { class: "section-title", text: t("field.quality") }));
    const bar = el("div", { class: "quality-bar" }, [el("div", { style: { width: `${Math.min(100, entry.quality)}%` } })]);
    detailNode.append(bar);
    detailNode.append(el("div", { class: "hint", text: t("detail.qualityText", { score: entry.quality, bits: entry.qualityBits.toFixed(1) }) }));
}

function renderStatus() {
    if (!state.info) {
        qs("#status-db").textContent = t("status.notOpen");
        qs("#status-counts").textContent = "";
        return;
    }
    const suffix = state.info.modified ? " *" : "";
    qs("#status-db").textContent = `${state.info.name}${suffix} — ${state.info.path}`;
    qs("#status-counts").textContent =
        t("status.summaryFull", { entries: state.info.entryCount, groups: state.info.groupCount, cipher: state.info.cipher, kdf: state.info.kdfName, version: state.info.formatName || state.info.versionLabel, security: state.info.securityLevel });
    // Warn when deletions are permanent, so it cannot be missed. Clicking the warning opens
    // the database settings, where the recycle bin can be switched back on.
    const warning = qs("#status-warning");
    const binOff = state.info.recycleBinEnabled === false;
    warning.textContent = binOff ? t("status.recycleBinOff") : "";
    warning.classList.toggle("clickable", binOff);
    warning.title = binOff ? t("status.recycleBinOffHint") : "";
}

function startClipboardTicker() {
    setInterval(async () => {
        const [active, seconds] = await vault.clipboardStatus();
        qs("#status-clipboard").textContent = active ? t("status.clipboard", { seconds: seconds }) : "";
    }, 1000);
}

// startAutoLock locks the workspace after the configured number of idle seconds, which
// mirrors the "lock after inactivity" option of KeePass.
function startAutoLock() {
    let idleSince = Date.now();
    const reset = () => { idleSince = Date.now(); };
    ["mousemove", "mousedown", "keydown", "wheel", "touchstart"].forEach((event) =>
        window.addEventListener(event, reset, { passive: true }));
    setInterval(() => {
        if (!state.info) return;
        const seconds = state.config.autoLockSeconds || 0;
        if (seconds <= 0) return;
        if (Date.now() - idleSince >= seconds * 1000) {
            reset();
            toast(t("toast.autoLocked"), "info");
            runCommand("file:lock");
        }
    }, 2000);
}

// ---------------------------------------------------------------- commands

function handleShortcut(event) {
    const ctrl = event.ctrlKey || event.metaKey;
    if (!ctrl) {
        if (event.key === "F5") {
            event.preventDefault();
            refreshAll();
        }
        if (event.key === "Delete" && state.selectedEntryId && !isEditing()) {
            event.preventDefault();
            // Shift+Delete skips the recycle bin, exactly like in KeePass.
            deleteEntry(state.entries.find((e) => e.id === state.selectedEntryId), event.shiftKey);
        }
        return;
    }
    const key = event.key.toLowerCase();
    const commands = {
        "n": "file:new",
        "o": "file:open",
    };
    if (event.shiftKey && key === "n") {
        event.preventDefault();
        if (state.info) runCommand("edit:new-entry");
        return;
    }
    if (event.shiftKey && key === "g") {
        event.preventDefault();
        if (state.info) runCommand("edit:new-group");
        return;
    }
    if (event.shiftKey && key === "s") {
        event.preventDefault();
        if (state.info) runCommand("file:save-as");
    } else if (key === "s") {
        event.preventDefault();
        if (state.info) runCommand("file:save");
    } else if (key === "n") {
        event.preventDefault();
        runCommand(commands.n);
    } else if (key === "o") {
        event.preventDefault();
        runCommand(commands.o);
    } else if (key === "l") {
        event.preventDefault();
        if (state.info) runCommand("file:lock");
    } else if (key === "w") {
        event.preventDefault();
        if (state.info) runCommand("file:close");
    } else if (key === "k") {
        event.preventDefault();
        if (state.selectedEntryId) runCommand("edit:duplicate");
    } else if (key === "f" && event.shiftKey) {
        event.preventDefault();
        if (state.info) runCommand("edit:find-all");
    } else if (key === "f") {
        event.preventDefault();
        if (state.info) qs("#search").focus();
    } else if (key === "g") {
        event.preventDefault();
        if (state.info) runCommand("tools:generator");
    } else if (key === "b") {
        event.preventDefault();
        if (state.selectedEntryId) runCommand("edit:copy-user");
    } else if (key === "u" && event.shiftKey) {
        event.preventDefault();
        if (state.selectedEntryId) runCommand("edit:open-url");
    } else if (key === "u") {
        event.preventDefault();
        if (state.selectedEntryId) runCommand("edit:copy-url");
    } else if (key === "c" && event.shiftKey) {
        event.preventDefault();
        runCommand("tools:clear-clipboard");
    } else if (key === "c" && state.selectedEntryId) {
        event.preventDefault();
        runCommand("edit:copy-password");
    }
}

function isEditing() {
    const active = document.activeElement;
    return active && (active.tagName === "INPUT" || active.tagName === "TEXTAREA");
}

async function runCommand(command) {
    switch (command) {
        case "file:new":
            openNewDatabaseDialog();
            return;
        case "file:open":
            if (state.info && state.info.modified &&
                !await confirmDialog({ title: t("confirm.unsavedTitle"), message: t("confirm.unsavedOpen") })) {
                return;
            }
            const path = await application.openFileDialog(t("fileDialog.openDatabase"), "*.kdbx;*.gkdb", "");
            if (!path) return;
            const password = await prompt({ title: t("dialog.masterPassword"), label: path, type: "password", okLabel: t("lock.unlock") });
            if (password === null) return;
            state.info = null;
            renderLock();
            await openDatabase(path, password, "");
            return;
        case "file:save":
            await saveDatabase();
            return;
        case "file:save-as": {
            const target = await application.saveFileDialog(t("fileDialog.saveAs"), suggestName(), "*.kdbx;*.gkdb");
            if (!target) return;
            await vault.saveAs(target);
            await refreshAll();
            toast(t("toast.savedAsPath", { path: target }), "success");
            return;
        }
        case "file:close":
            if (!await confirmClose()) return;
            await vault.close(false);
            state.info = null;
            setToolbarEnabled(false);
            renderLock();
            return;
        case "file:lock":
            await vault.lock();
            state.info = null;
            state.detail = null;
            setToolbarEnabled(false);
            renderLock();
            return;
        case "file:import-csv": {
            if (!state.info) return;
            const file = await application.openFileDialog(t("fileDialog.importCsv"), "*.csv", "");
            if (!file) return;
            const count = await vault.importCsv(file, state.selectedGroupId);
            await refreshAll();
            toast(t("toast.imported", { count: count }), "success");
            return;
        }
        case "file:export-csv": {
            if (!state.info) return;
            const file = await application.saveFileDialog(t("fileDialog.exportCsv"), "export.csv", "*.csv");
            if (!file) return;
            const count = await vault.exportCsv(file);
            toast(t("toast.exported", { count: count }), "success");
            return;
        }
        case "file:backup": {
            if (!state.info) return;
            const file = await application.saveFileDialog(t("fileDialog.backup"), suggestName("backup"), "*.kdbx;*.gkdb");
            if (!file) return;
            await vault.backup(file);
            toast(t("toast.backupDone"), "success");
            return;
        }
        case "file:recent-clear":
            await application.clearRecent();
            toast(t("toast.recentCleared"));
            return;
        case "file:quit":
            if (!await confirmClose()) return;
            await application.quit(true);
            return;
        case "app:closing":
            // Windows asked to close the window (Alt+F4, taskbar, title bar button).
            if (await confirmClose()) {
                await application.quit(true);
            }
            return;
        case "edit:new-entry":
            if (!state.info) return;
            openEntryEditor(null, currentGroupId());
            return;
        case "edit:new-group":
            if (!state.info) return;
            createGroupDialog(currentGroupId());
            return;
        case "edit:duplicate":
            if (state.selectedEntryId) await duplicateEntry(state.selectedEntryId);
            return;
        case "edit:delete":
            if (state.selectedEntryId) await deleteEntry(state.entries.find((e) => e.id === state.selectedEntryId));
            return;
        case "edit:copy-user":
            if (state.selectedEntryId) await copyField(state.selectedEntryId, "UserName");
            return;
        case "edit:copy-password":
            if (state.selectedEntryId) await copyField(state.selectedEntryId, "Password");
            return;
        case "edit:copy-url":
            if (state.selectedEntryId) await copyField(state.selectedEntryId, "URL");
            return;
        case "edit:open-url":
            if (state.selectedEntryId) await openUrl(state.selectedEntryId);
            return;
        case "edit:find":
        case "edit:find-all":
            openSearchDialog(command === "edit:find-all");
            return;
        case "view:refresh":
            await refreshAll();
            return;
        case "view:recycle-bin": {
            await setRecycleBinVisible(!state.showRecycleBin);
            toast(state.showRecycleBin ? t("toast.recycleShown") : t("toast.recycleHidden"));
            return;
        }
        case "view:show-passwords":
            toast(t("detail.passwordHint"), "info");
            return;
        case "view:theme": {
            const next = state.config.theme === "light" ? "dark" : "light";
            state.config.theme = next;
            // applyTheme stores the choice and updates the native window decoration.
            applyTheme(next);
            return;
        }
        case "tools:generator":
            openGeneratorDialog();
            return;
        case "tools:db-settings":
            await openDatabaseSettingsDialog();
            return;
        case "tools:change-key":
            openChangeKeyDialog();
            return;
        case "tools:key-file": {
            const file = await application.saveFileDialog(t("fileDialog.createKeyFile"), "NewKey.keyx", "*.keyx");
            if (!file) return;
            await vault.createKeyFile(file);
            toast(t("toast.keyFileCreated", { path: file }), "success");
            return;
        }
        case "tools:duplicates":
            await showDuplicateDialog();
            return;
        case "tools:expired":
            await showExpiredDialog();
            return;
        case "tools:empty-bin": {
            if (!state.info) return;
            if (!await confirmDialog({
                title: t("menu.emptyBin"),
                message: t("confirm.emptyBinMessage"),
                danger: true,
                okLabel: t("confirm.emptyBin"),
            })) return;
            await vault.emptyRecycleBin();
            await refreshAll();
            toast(t("toast.recycleEmptied"), "success");
            return;
        }
        case "tools:clear-clipboard":
            await vault.clearClipboard();
            toast(t("toast.clipboardCleared"));
            return;
        case "tools:options":
            openOptionsDialog();
            return;
        case "help:docs":
            await application.openExternal("https://keepass.info/help/base/index.html");
            return;
        case "help:keepass":
            await application.openExternal("https://keepass.info/");
            return;
        case "help:about":
            openAboutDialog();
            return;
        default:
            if (command.startsWith("file:recent:")) {
                const path = command.substring("file:recent:".length);
                const password = await prompt({ title: t("dialog.masterPassword"), label: path, type: "password", okLabel: t("lock.unlock") });
                if (password === null) return;
                await openDatabase(path, password, "");
            }
    }
}

function currentGroupId() {
    if (state.selectedGroupId) return state.selectedGroupId;
    return state.groups.length ? state.groups[0].id : "";
}

function suggestName(suffix = "") {
    const base = state.info && state.info.name ? state.info.name : "Database";
    return suffix ? `${base}-${suffix}.kdbx` : `${base}.kdbx`;
}

async function confirmClose() {
    if (!state.info) return true;
    if (!state.info.modified) return true;
    return application.questionDialog(t("confirm.unsavedTitle"), t("confirm.unsavedMessage"));
}

async function saveDatabase() {
    if (!state.info) return;
    try {
        await vault.save();
        await refreshAll();
        toast(t("toast.saved"), "success");
    } catch (error) {
        /* reported */
    }
}

async function copyField(entryId, field) {
    try {
        await vault.copyField(entryId, field);
        toast(t("toast.copied", { field: field }), "success");
    } catch (error) {
        /* reported */
    }
}

async function openUrl(entryId) {
    const entry = state.detail && state.detail.id === entryId ? state.detail : await vault.entry(entryId);
    const target = (entry.overrideUrl || entry.url || "").trim();
    if (!target) {
        toast(t("toast.noUrl"), "error");
        return;
    }
    await vault.touch(entryId);
    await application.openExternal(target);
}

async function duplicateEntry(entryId) {
    const id = await vault.duplicateEntry(entryId);
    await refreshAll();
    await selectEntry(id);
}

async function restoreEntry(entryId) {
    await vault.restoreToPreviousGroup(entryId);
    state.selectedEntryId = "";
    state.detail = null;
    await refreshAll();
    renderDetail();
    toast(t("toast.restored"), "success");
}

async function restoreToGroupDialog(entryId) {
    const groups = (await vault.groups(false)).filter((g) => !inRecycleBin(g.id));
    const select = el("select");
    for (const group of groups) {
        const label = group.name || t("group.rootLabel");
        select.append(el("option", { attrs: { value: group.id }, text: label }));
    }
    const ok = el("button", { class: "btn primary", text: t("restore.action") });
    const cancel = el("button", { class: "btn", text: t("dialog.cancel") });
    const dialog = openModal({
        title: t("restore.title"),
        width: "small",
        body: el("div", { class: "grid" }, [el("label", { text: t("restore.target") }), select]),
        footer: [cancel, ok],
    });
    cancel.addEventListener("click", () => dialog.close());
    ok.addEventListener("click", async () => {
        await vault.restoreEntry(entryId, select.value);
        dialog.close();
        state.selectedEntryId = "";
        state.detail = null;
        await refreshAll();
        renderDetail();
        toast(t("toast.restoredGeneric"), "success");
    });
}

async function deleteEntry(entry, forcePermanent = false) {
    if (!entry) return;
    const toBin = !forcePermanent && !entry.isRecycled && state.info.recycleBinEnabled;
    const message = toBin
        ? t("confirm.deleteToBin", { title: entry.title })
        : t("confirm.deletePermanent", { title: entry.title });
    if (state.config.confirmDelete && !await confirmDialog({
        title: t("confirm.deleteTitle"), message, danger: !toBin, okLabel: toBin ? t("confirm.moveToBin") : t("confirm.deleteForever"),
    })) return;
    await vault.deleteEntry(entry.id, !toBin);
    state.selectedEntryId = "";
    state.detail = null;
    await refreshAll();
    renderDetail();
    // Say what actually happened: without the recycle bin every deletion is final, which is
    // easy to miss when the bin was switched off in the database settings.
    if (toBin) {
        if (!state.showRecycleBin) {
            // Never hide the entry the user just deleted.
            await setRecycleBinVisible(true);
        }
        toast(t("toast.movedToBin", { title: entry.title }), "success");
    } else {
        toast(t("toast.deletedForever", { title: entry.title }), "warning");
    }
}

async function deleteGroup(group, forcePermanent = false) {
    if (!group) return;
    const toBin = !forcePermanent && !group.isRecycled && state.info.recycleBinEnabled;
    if (!await confirmDialog({
        title: t("confirm.deleteGroupTitle"),
        message: toBin
            ? t("confirm.deleteGroupMessage", { name: group.name })
            : t("confirm.deleteGroupPermanent", { name: group.name }),
        danger: !toBin,
        okLabel: toBin ? t("confirm.moveToBin") : t("confirm.deleteForever"),
    })) return;
    await vault.deleteGroup(group.id, false);
    state.selectedGroupId = await vault.rootId();
    await refreshAll();
    if (toBin) {
        if (!state.showRecycleBin) {
            // Never hide the group the user just deleted.
            await setRecycleBinVisible(true);
        }
        toast(t("toast.movedToBin", { title: group.name }), "success");
    } else {
        toast(t("toast.deletedForever", { title: group.name }), "warning");
    }
}

function createGroupDialog(parentId) {
    const nameInput = el("input", { attrs: { type: "text" } });
    const iconSelect = iconSelectNode(48);
    const ok = el("button", { class: "btn primary", text: t("group.createAction") });
    const cancel = el("button", { class: "btn", text: t("dialog.cancel") });
    const dialog = openModal({
        title: t("group.create"),
        width: "small",
        body: el("div", { class: "grid" }, [
            el("label", { text: t("settings.name") }), nameInput,
            el("label", { text: t("group.icon") }), iconSelect,
        ]),
        footer: [cancel, ok],
    });
    cancel.addEventListener("click", () => dialog.close());
    ok.addEventListener("click", async () => {
        if (!nameInput.value.trim()) {
            toast(t("group.emptyName"), "error");
            return;
        }
        const id = await vault.createGroup(parentId, nameInput.value.trim(), Number(iconSelect.value));
        dialog.close();
        await refreshAll();
        await selectGroup(id);
    });
}

function iconSelectNode(selected) {
    const select = el("select");
    for (const [id, name] of Object.entries(state.iconNames)) {
        select.append(el("option", { attrs: { value: id }, text: `${id} - ${name}` }));
    }
    select.value = String(selected);
    return select;
}

function openGroupDialog(group) {
    const nameInput = el("input", { attrs: { type: "text", value: group.name } });
    const notesInput = el("textarea");
    notesInput.value = group.notes || "";
    const iconSelect = iconSelectNode(group.iconId);
    const expires = el("input", { attrs: { type: "checkbox" } });
    expires.checked = !!group.expires;
    const expiryInput = el("input", { attrs: { type: "datetime-local", value: toLocalInput(group.expiryTime) } });
    const tagsInput = el("input", { attrs: { type: "text", value: (group.tags || []).join("; ") } });
    const autoType = triStateSelect(group.enableAutoType);
    const searching = triStateSelect(group.enableSearching);
    const ok = el("button", { class: "btn primary", text: t("menu.save") });
    const cancel = el("button", { class: "btn", text: t("dialog.cancel") });
    const dialog = openModal({
        title: t("group.title"),
        body: el("div", { class: "grid" }, [
            el("label", { text: t("settings.name") }), nameInput,
            el("label", { text: t("group.icon") }), iconSelect,
            el("label", { text: t("group.tags") }), tagsInput,
            el("label", { text: t("group.notes") }), notesInput,
            el("label", { text: t("group.autoType") }), autoType,
            el("label", { text: t("group.searching") }), searching,
            el("label", { text: t("group.expires") }), el("div", { class: "row" }, [expires, expiryInput]),
        ]),
        footer: [cancel, ok],
    });
    cancel.addEventListener("click", () => dialog.close());
    ok.addEventListener("click", async () => {
        const dto = Object.assign({}, group, {
            name: nameInput.value.trim(),
            notes: notesInput.value,
            iconId: Number(iconSelect.value),
            tags: tagsInput.value.split(";").map((t) => t.trim()).filter(Boolean),
            expires: expires.checked,
            expiryTime: expiryInput.value ? new Date(expiryInput.value).toISOString() : "",
            enableAutoType: parseTriState(autoType.value),
            enableSearching: parseTriState(searching.value),
        });
        await vault.updateGroup(dto);
        dialog.close();
        await refreshAll();
    });
}

function triStateSelect(value) {
    const select = el("select");
    select.append(el("option", { attrs: { value: "inherit" }, text: t("group.inherit") }));
    select.append(el("option", { attrs: { value: "true" }, text: t("group.enabled") }));
    select.append(el("option", { attrs: { value: "false" }, text: t("group.disabled") }));
    select.value = value === null || value === undefined ? "inherit" : String(value);
    return select;
}

function parseTriState(value) {
    if (value === "true") return true;
    if (value === "false") return false;
    return null;
}

function toLocalInput(value) {
    if (!value) return "";
    const date = new Date(value);
    if (isNaN(date.getTime())) return "";
    const pad = (n) => String(n).padStart(2, "0");
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

// ---------------------------------------------------------------- entry editor

async function openEntryEditor(entryId, groupId) {
    const isNew = !entryId;
    const dto = isNew
        ? {
            id: "", groupId: groupId || currentGroupId(), title: "", userName: "", password: "", url: "",
            notes: "", iconId: 0, tags: [], fields: [], attachments: [], history: [],
            autoTypeEnabled: true, autoTypeSequence: "{USERNAME}{TAB}{PASSWORD}{ENTER}", autoTypeItems: [],
            expires: false, expiryTime: "", quality: 0, qualityBits: 0,
        }
        : await vault.entry(entryId);

    const titleInput = el("input", { attrs: { type: "text", value: dto.title } });
    const userInput = el("input", { attrs: { type: "text", value: dto.userName } });
    const passwordInput = el("input", { attrs: { type: "password", value: dto.password } });
    const urlInput = el("input", { attrs: { type: "text", value: dto.url } });
    const notesInput = el("textarea");
    notesInput.value = dto.notes || "";
    const tagsInput = el("input", { attrs: { type: "text", value: (dto.tags || []).join("; ") } });
    const iconSelect = iconSelectNode(dto.iconId || 0);
    const expires = el("input", { attrs: { type: "checkbox" } });
    expires.checked = !!dto.expires;
    const expiryInput = el("input", { attrs: { type: "datetime-local", value: toLocalInput(dto.expiryTime) } });
    const overrideInput = el("input", { attrs: { type: "text", value: dto.overrideUrl || "" } });
    const fgInput = el("input", { attrs: { type: "text", value: dto.foregroundColor || "", placeholder: "#RRGGBB" } });
    const bgInput = el("input", { attrs: { type: "text", value: dto.backgroundColor || "", placeholder: "#RRGGBB" } });
    const qualityBar = el("div", { class: "quality-bar" }, [el("div")]);
    const qualityLabel = el("div", { class: "hint" });

    const reveal = el("button", { class: "btn", text: t("editor.show") });
    reveal.addEventListener("click", () => {
        const isPassword = passwordInput.type === "password";
        passwordInput.type = isPassword ? "text" : "password";
        reveal.textContent = isPassword ? t("editor.hide") : t("editor.show");
    });
    const generate = el("button", { class: "btn", text: t("editor.generate") });
    generate.addEventListener("click", () => openGeneratorDialog((password) => {
        passwordInput.value = password;
        passwordInput.dispatchEvent(new Event("input"));
    }));

    const updateQuality = async () => {
        const result = await vault.evaluate(passwordInput.value);
        const [, score] = result;
        qualityBar.firstChild.style.width = `${Math.min(100, score)}%`;
        qualityLabel.textContent = t("editor.strength", { label: qualityText(score), score: score });
    };
    passwordInput.addEventListener("input", updateQuality);
    if (dto.password) updateQuality();

    // Custom fields -------------------------------------------------------
    const fieldsBox = el("div", { class: "list-box" });
    const fields = (dto.fields || []).map((f) => Object.assign({}, f));
    const renderFields = () => {
        clear(fieldsBox);
        if (fields.length === 0) {
            fieldsBox.append(el("div", { class: "item hint", text: t("editor.noFields") }));
        }
        fields.forEach((field, index) => {
            const key = el("input", { attrs: { type: "text", value: field.key } });
            key.style.width = "150px";
            const value = el("input", { attrs: { type: field.protected ? "password" : "text", value: field.value } });
            value.style.flex = "1";
            const protect = el("input", { attrs: { type: "checkbox" } });
            protect.checked = !!field.protected;
            const remove = el("button", { class: "icon-btn", text: "✕" });
            key.addEventListener("input", () => { field.key = key.value; });
            value.addEventListener("input", () => { field.value = value.value; });
            protect.addEventListener("change", () => {
                field.protected = protect.checked;
                value.type = protect.checked ? "password" : "text";
            });
            remove.addEventListener("click", () => {
                fields.splice(index, 1);
                renderFields();
            });
            fieldsBox.append(el("div", { class: "item" }, [
                key, value,
                el("label", { class: "checkbox hint" }, [protect, t("editor.protect")]),
                remove,
            ]));
        });
    };
    renderFields();
    const addField = el("button", { class: "btn", text: t("editor.addField") });
    addField.addEventListener("click", () => {
        fields.push({ key: "NewField", value: "", protected: false });
        renderFields();
    });

    // Attachments ---------------------------------------------------------
    const attachmentsBox = el("div", { class: "list-box" });
    const renderAttachments = () => {
        clear(attachmentsBox);
        if (!dto.attachments || dto.attachments.length === 0) {
            attachmentsBox.append(el("div", { class: "item hint", text: t("editor.noAttachments") }));
        }
        for (const attachment of dto.attachments || []) {
            const remove = el("button", { class: "icon-btn", text: t("menu.delete") });
            remove.addEventListener("click", async () => {
                if (!entryId) {
                    toast(t("toast.needSaveBeforeDelete"), "error");
                    return;
                }
                await vault.removeAttachment(entryId, attachment.key);
                dto.attachments = (await vault.entry(entryId)).attachments;
                renderAttachments();
            });
            const exportButton = el("button", { class: "icon-btn", text: t("editor.exportAttachment") });
            exportButton.addEventListener("click", async () => {
                if (!entryId) return;
                const target = await application.saveFileDialog(t("fileDialog.exportAttachment"), attachment.key, "*");
                if (!target) return;
                await vault.exportAttachment(entryId, attachment.key, target);
                toast(t("toast.attachmentExported"), "success");
            });
            attachmentsBox.append(el("div", { class: "item" }, [
                el("span", { class: "grow", text: t("field.attachmentSize", { name: attachment.key, size: attachment.size }) }),
                exportButton, remove,
            ]));
        }
    };
    renderAttachments();
    const addAttachment = el("button", { class: "btn", text: t("editor.addAttachment") });
    addAttachment.addEventListener("click", async () => {
        if (!entryId) {
            toast(t("toast.needSaveBeforeAttachment"), "error");
            return;
        }
        const file = await application.openFileDialog(t("fileDialog.selectAttachment"), "*", "");
        if (!file) return;
        const data = await application.readFile(file);
        const name = file.split(/[\\/]/).pop();
        await vault.addAttachment(entryId, name, data);
        dto.attachments = (await vault.entry(entryId)).attachments;
        renderAttachments();
    });

    // Auto type -----------------------------------------------------------
    const autoEnabled = el("input", { attrs: { type: "checkbox" } });
    autoEnabled.checked = dto.autoTypeEnabled !== false;
    const autoSequence = el("input", { attrs: { type: "text", value: dto.autoTypeSequence || "" } });
    const associations = (dto.autoTypeItems || []).map((a) => Object.assign({}, a));
    const autoBox = el("div", { class: "list-box" });
    const renderAssociations = () => {
        clear(autoBox);
        if (associations.length === 0) {
            autoBox.append(el("div", { class: "item hint", text: t("editor.noRules") }));
        }
        associations.forEach((item, index) => {
            const window = el("input", { attrs: { type: "text", value: item.window, placeholder: t("editor.windowPattern") } });
            const sequence = el("input", { attrs: { type: "text", value: item.sequence, placeholder: "{USERNAME}{TAB}{PASSWORD}{ENTER}" } });
            window.addEventListener("input", () => { item.window = window.value; });
            sequence.addEventListener("input", () => { item.sequence = sequence.value; });
            const remove = el("button", { class: "icon-btn", text: "✕" });
            remove.addEventListener("click", () => {
                associations.splice(index, 1);
                renderAssociations();
            });
            autoBox.append(el("div", { class: "item" }, [window, sequence, remove]));
        });
    };
    renderAssociations();
    const addAssociation = el("button", { class: "btn", text: t("editor.addRule") });
    addAssociation.addEventListener("click", () => {
        associations.push({ window: "", sequence: "{USERNAME}{TAB}{PASSWORD}{ENTER}" });
        renderAssociations();
    });

    // History -------------------------------------------------------------
    const historyBox = el("div", { class: "list-box" });
    if (!dto.history || dto.history.length === 0) {
        historyBox.append(el("div", { class: "item hint", text: t("editor.noHistory") }));
    }
    (dto.history || []).forEach((revision, index) => {
        const restore = el("button", { class: "icon-btn", text: t("detail.restore") });
        restore.addEventListener("click", async () => {
            if (!entryId) return;
            await vault.restoreHistory(entryId, index);
            dialog.close();
            await refreshAll();
            await selectEntry(entryId);
        });
        const remove = el("button", { class: "icon-btn", text: t("menu.delete") });
        remove.addEventListener("click", async () => {
            if (!entryId) return;
            await vault.deleteHistory(entryId, index);
            dialog.close();
            await openEntryEditor(entryId);
        });
        historyBox.append(el("div", { class: "item" }, [
            el("span", { class: "grow", text: t("detail.historyLine", { date: formatDate(revision.modified), title: revision.title || t("detail.noTitle") }) }),
            restore, remove,
        ]));
    });

    const tabs = [
        [t("editor.tabGeneral"), el("div", { class: "grid" }, [
            el("label", { text: t("field.title") }), titleInput,
            el("label", { text: t("field.userName") }), userInput,
            el("label", { text: t("field.password") }),
            el("div", { class: "row" }, [passwordInput, reveal, generate]),
            el("div", { class: "full" }, [qualityBar, qualityLabel]),
            el("label", { text: t("field.url") }), urlInput,
            el("label", { text: t("group.notes") }), notesInput,
            el("label", { text: t("group.tags") }), tagsInput,
            el("label", { text: t("group.icon") }), iconSelect,
            el("label", { text: t("field.foreground") }), fgInput,
            el("label", { text: t("field.background") }), bgInput,
            el("label", { text: t("field.overrideUrl") }), overrideInput,
            el("label", { text: t("group.expires") }), el("div", { class: "row" }, [expires, expiryInput]),
        ])],
        [t("editor.tabFields"), el("div", {}, [fieldsBox, el("div", { style: { marginTop: "8px" } }, [addField])])],
        [t("editor.tabAttachments"), el("div", {}, [attachmentsBox, el("div", { style: { marginTop: "8px" } }, [addAttachment])])],
        [t("group.autoType"), el("div", { class: "grid" }, [
            el("label", { text: t("group.enabled") }), autoEnabled,
            el("label", { text: t("editor.defaultSequence") }), autoSequence,
            el("div", { class: "full" }, [autoBox]),
            el("div", { class: "full" }, [addAssociation]),
        ])],
        [t("editor.tabHistory"), historyBox],
    ];
    const body = el("div");
    const tabBar = el("div", { class: "tabs" });
    const panels = [];
    tabs.forEach(([label, node], index) => {
        const tab = el("div", { class: "tab" + (index === 0 ? " active" : ""), text: label });
        tab.addEventListener("click", () => {
            tabBar.querySelectorAll(".tab").forEach((t) => t.classList.remove("active"));
            tab.classList.add("active");
            panels.forEach((panel, i) => { panel.style.display = i === index ? "" : "none"; });
        });
        tabBar.append(tab);
        node.style.display = index === 0 ? "" : "none";
        panels.push(node);
        body.append(node);
    });

    const save = el("button", { class: "btn primary", text: t("menu.save") });
    const cancel = el("button", { class: "btn", text: t("dialog.cancel") });
    const dialog = openModal({
        title: isNew ? t("group.menuNewEntry") : t("editor.titleEdit", { title: dto.title || "" }),
        width: "wide",
        body: [tabBar, body],
        footer: [cancel, save],
    });
    cancel.addEventListener("click", () => dialog.close());
    save.addEventListener("click", async () => {
        const payload = Object.assign({}, dto, {
            title: titleInput.value,
            userName: userInput.value,
            password: passwordInput.value,
            url: urlInput.value,
            notes: notesInput.value,
            tags: tagsInput.value.split(";").map((t) => t.trim()).filter(Boolean),
            iconId: Number(iconSelect.value),
            foregroundColor: fgInput.value,
            backgroundColor: bgInput.value,
            overrideUrl: overrideInput.value,
            expires: expires.checked,
            expiryTime: expiryInput.value ? new Date(expiryInput.value).toISOString() : "",
            fields: fields.filter((f) => f.key && f.key.trim() !== ""),
            autoTypeEnabled: autoEnabled.checked,
            autoTypeSequence: autoSequence.value,
            autoTypeItems: associations.filter((a) => (a.window || "").trim() !== ""),
        });
        try {
            if (isNew) {
                const id = await vault.createEntry(dto.groupId || currentGroupId(), payload);
                dialog.close();
                await refreshAll();
                await selectEntry(id);
            } else {
                await vault.updateEntry(payload);
                dialog.close();
                await refreshAll();
                await selectEntry(entryId);
            }
            toast(t("toast.saved"), "success");
        } catch (error) {
            /* reported */
        }
    });
    setTimeout(() => titleInput.focus(), 40);
}

async function exportAttachment(name) {
    if (!state.selectedEntryId) return;
    const target = await application.saveFileDialog(t("fileDialog.exportAttachment"), name, "*");
    if (!target) return;
    await vault.exportAttachment(state.selectedEntryId, name, target);
    toast(t("toast.attachmentExported"), "success");
}

async function moveDialog(id, isGroup) {
    const groups = (await vault.groups(true)).filter((g) => g.id !== id);
    const select = el("select");
    for (const group of groups) {
        select.append(el("option", { attrs: { value: group.id }, text: group.name }));
    }
    const ok = el("button", { class: "btn primary", text: t("move.action") });
    const cancel = el("button", { class: "btn", text: t("dialog.cancel") });
    const dialog = openModal({
        title: t("move.title"),
        width: "small",
        body: el("div", { class: "grid" }, [el("label", { text: t("restore.target") }), select]),
        footer: [cancel, ok],
    });
    cancel.addEventListener("click", () => dialog.close());
    ok.addEventListener("click", async () => {
        if (isGroup) {
            await vault.moveGroup(id, select.value);
        } else {
            await vault.moveEntry(id, select.value);
        }
        dialog.close();
        await refreshAll();
    });
}

// ---------------------------------------------------------------- generator

function openGeneratorDialog(onAccept) {
    const cfg = state.config && state.config.generator ? state.config.generator : {
        length: 20, upper: true, lower: true, digits: true, special: true,
        minus: false, under: false, space: false, brackets: false, hex: false,
        excludeLookAlike: false, eachGroupAtLeast: 0, noRepeating: false, customSet: "",
        pattern: "", usePattern: false,
    };
    const options = Object.assign({}, cfg);

    const preview = el("div", { class: "password-box" });
    const qualityBar = el("div", { class: "quality-bar" }, [el("div")]);
    const qualityLabel = el("div", { class: "hint" });
    const lengthInput = el("input", { attrs: { type: "number", min: "1", max: "256", value: String(options.length) } });
    const eachGroup = el("input", { attrs: { type: "number", min: "0", max: "10", value: String(options.eachGroupAtLeast) } });
    const customSet = el("input", { attrs: { type: "text", value: options.customSet, placeholder: t("generator.customSetPlaceholder") } });
    const patternInput = el("input", { attrs: { type: "text", value: options.pattern, placeholder: t("generator.patternHint") } });

    const toggles = {};
    const toggleRow = el("div", { class: "row wrap" });
    const definitions = [
        ["upper", t("generator.upper")], ["lower", t("generator.lower")], ["digits", t("generator.digits")],
        ["special", t("generator.special")], ["minus", t("generator.minus")], ["under", t("generator.under")],
        ["space", t("generator.space")], ["brackets", t("generator.brackets")], ["hex", t("generator.hex")],
        ["excludeLookAlike", t("generator.excludeLookAlike")], ["noRepeating", t("generator.noRepeating")], ["usePattern", t("generator.usePattern")],
    ];
    for (const [key, label] of definitions) {
        const box = el("input", { attrs: { type: "checkbox" } });
        box.checked = !!options[key];
        box.addEventListener("change", () => {
            options[key] = box.checked;
            regenerate();
        });
        toggles[key] = box;
        toggleRow.append(el("label", { class: "checkbox" }, [box, label]));
    }

    const regenerate = async () => {
        options.length = Number(lengthInput.value) || 20;
        options.eachGroupAtLeast = Number(eachGroup.value) || 0;
        options.customSet = customSet.value.trim();
        options.pattern = patternInput.value;
        const result = await generator.generate(options);
        if (result.error) {
            preview.textContent = result.error;
            qualityLabel.textContent = "";
            qualityBar.firstChild.style.width = "0";
            return;
        }
        preview.textContent = result.password;
        qualityBar.firstChild.style.width = `${Math.min(100, result.score)}%`;
        qualityLabel.textContent = t("generator.qualityLine", {
            label: qualityText(result.score), score: result.score, bits: result.bits.toFixed(1),
        });
    };
    [lengthInput, eachGroup, customSet, patternInput].forEach((input) =>
        input.addEventListener("input", regenerate));
    regenerate();

    const copy = el("button", { class: "btn", text: t("generator.copy") });
    copy.addEventListener("click", async () => {
        await vault.copyField(state.selectedEntryId || "", "__none__").catch(() => {});
        navigator.clipboard.writeText(preview.textContent);
        toast(t("toast.copiedToClipboard"), "success");
    });
    const use = el("button", { class: "btn primary", text: onAccept ? t("generator.use") : t("dialog.close") });
    const save = el("button", { class: "btn", text: t("generator.saveDefaults") });
    const dialog = openModal({
        title: t("generator.title"),
        body: el("div", {}, [
            preview,
            qualityBar,
            qualityLabel,
            el("div", { class: "section-title", text: t("options.title") }),
            el("div", { class: "grid" }, [
                el("label", { text: t("generator.length") }), lengthInput,
                el("label", { text: t("generator.customSet") }), customSet,
                el("label", { text: t("generator.eachGroup") }), eachGroup,
                el("label", { text: t("generator.pattern") }), patternInput,
                el("div", { class: "full" }, [toggleRow]),
            ]),
        ]),
        footer: [copy, save, use],
    });
    save.addEventListener("click", async () => {
        await generator.saveDefaults(options);
        const config = await application.config();
        config.generator = options;
        state.config = config;
        toast(t("toast.generatorDefaults"), "success");
    });
    use.addEventListener("click", () => {
        if (onAccept) onAccept(preview.textContent);
        dialog.close();
    });
}

// ---------------------------------------------------------------- database settings

async function openDatabaseSettingsDialog() {
    const settings = await vault.settings();
    const nameInput = el("input", { attrs: { type: "text", value: settings.name || "" } });
    const descInput = el("textarea");
    descInput.value = settings.description || "";
    const userInput = el("input", { attrs: { type: "text", value: settings.defaultUserName || "" } });
    // The native format uses authenticated ciphers, the KDBX format the classic ones.
    const native = settings.format === "gokeep";
    const cipherSelect = native
        ? selectFrom(["AES-256-GCM", "ChaCha20-Poly1305"],
            settings.cipher === "ChaCha20" ? "ChaCha20-Poly1305" : "AES-256-GCM")
        : selectFrom(["AES-256", "ChaCha20"], settings.cipher);
    const compressionSelect = selectFrom(["GZip", "None"], settings.compression);
    const versionSelect = selectFrom(["KDBX 4.1", "KDBX 4.0", "KDBX 3.1"], versionLabelOf(settings.fileVersion));
    // Only KDBX files have a "file version"; the native format shows its version instead.
    const formatRow = native
        ? [el("label", { text: t("create.format") }),
           el("div", {}, [el("span", { class: "badge ok", text: (state.info && state.info.formatName) || "GoKeep v1" })])]
        : [el("label", { text: t("settings.fileVersion") }), versionSelect];
    const kdfSelect = selectFrom(["Argon2d", "Argon2id", "AES-KDF"], settings.kdfName);
    const memoryInput = el("input", { attrs: { type: "number", value: String(Math.round((settings.kdfMemory || 0) / 1048576)) } });
    const iterationsInput = el("input", { attrs: { type: "number", value: String(settings.kdfIterations || 0) } });
    const parallelismInput = el("input", { attrs: { type: "number", value: String(settings.kdfParallelism || 0) } });
    const roundsInput = el("input", { attrs: { type: "number", value: String(settings.kdfRounds || 0) } });
    const recycle = el("input", { attrs: { type: "checkbox" } });
    recycle.checked = !!settings.recycleBinEnabled;
    const historyItems = el("input", { attrs: { type: "number", value: String(settings.historyMaxItems || 0) } });
    const historySize = el("input", { attrs: { type: "number", value: String(Math.round((settings.historyMaxSize || 0) / 1024)) } });
    const historyDays = el("input", { attrs: { type: "number", value: String(settings.maintenanceHistoryDays || 0) } });
    const protections = {
        protectTitle: checkbox(settings.protectTitle),
        protectUserName: checkbox(settings.protectUserName),
        protectPassword: checkbox(settings.protectPassword),
        protectURL: checkbox(settings.protectUrl),
        protectNotes: checkbox(settings.protectNotes),
    };

    const ok = el("button", { class: "btn primary", text: t("menu.save") });
    const cancel = el("button", { class: "btn", text: t("dialog.cancel") });
    const dialog = openModal({
        title: t("settings.title"),
        width: "wide",
        body: el("div", { class: "grid" }, [
            el("label", { text: t("settings.name") }), nameInput,
            el("label", { text: t("settings.description") }), descInput,
            el("label", { text: t("settings.defaultUser") }), userInput,
            el("div", { class: "full" }, [el("div", { class: "section-title", text: t("settings.sectionSecurity") })]),
            el("label", { text: t("settings.cipher") }), cipherSelect,
            el("label", { text: t("settings.compression") }), compressionSelect,
            ...[formatRow],
            el("label", { text: t("settings.kdf") }), kdfSelect,
            el("label", { text: t("settings.memory") }), memoryInput,
            el("label", { text: t("settings.iterations") }), iterationsInput,
            el("label", { text: t("settings.parallelism") }), parallelismInput,
            el("label", { text: t("settings.rounds") }), roundsInput,
            el("div", { class: "full" }, [el("div", { class: "section-title", text: t("settings.sectionHistory") })]),
            el("label", { text: t("settings.recycleEnabled") }), recycle,
            el("label", { text: t("settings.historyItems") }), historyItems,
            el("label", { text: t("settings.historySize") }), historySize,
            el("label", { text: t("settings.historyDays") }), historyDays,
            el("div", { class: "full" }, [el("div", { class: "section-title", text: t("settings.sectionProtection") })]),
            el("label", { text: t("settings.protectTitle") }), protections.protectTitle,
            el("label", { text: t("settings.protectUser") }), protections.protectUserName,
            el("label", { text: t("settings.protectPassword") }), protections.protectPassword,
            el("label", { text: t("settings.protectUrl") }), protections.protectURL,
            el("label", { text: t("settings.protectNotes") }), protections.protectNotes,
            el("div", { class: "full hint", text: t("settings.kdfHint") }),
        ]),
        footer: [cancel, ok],
    });
    cancel.addEventListener("click", () => dialog.close());
    ok.addEventListener("click", async () => {
        const payload = {
            name: nameInput.value,
            description: descInput.value,
            defaultUserName: userInput.value,
            cipher: cipherSelect.value,
            compression: compressionSelect.value,
            fileVersion: fileVersionOf(versionSelect.value),
            kdfName: kdfSelect.value,
            kdfMemory: (Number(memoryInput.value) || 0) * 1048576,
            kdfIterations: Number(iterationsInput.value) || 0,
            kdfParallelism: Number(parallelismInput.value) || 0,
            kdfRounds: Number(roundsInput.value) || 0,
            recycleBinEnabled: recycle.checked,
            historyMaxItems: Number(historyItems.value) || 0,
            historyMaxSize: (Number(historySize.value) || 0) * 1024,
            maintenanceHistoryDays: Number(historyDays.value) || 0,
            protectTitle: protections.protectTitle.firstChild.checked,
            protectUserName: protections.protectUserName.firstChild.checked,
            protectPassword: protections.protectPassword.firstChild.checked,
            protectUrl: protections.protectURL.firstChild.checked,
            protectNotes: protections.protectNotes.firstChild.checked,
        };
        await vault.applySettings(payload);
        dialog.close();
        await refreshAll();
        toast(t("toast.settingsUpdated"), "success");
    });
}

function checkbox(checked) {
    const box = el("input", { attrs: { type: "checkbox" } });
    box.checked = !!checked;
    return el("label", { class: "checkbox" }, [box]);
}

function selectFrom(values, current) {
    const select = el("select");
    for (const value of values) {
        select.append(el("option", { attrs: { value }, text: value }));
    }
    select.value = current;
    return select;
}

function versionLabelOf(version) {
    if (version === 0x00030001) return "KDBX 3.1";
    if (version === 0x00040000) return "KDBX 4.0";
    return "KDBX 4.1";
}

function fileVersionOf(label) {
    if (label === "KDBX 3.1") return 0x00030001;
    if (label === "KDBX 4.0") return 0x00040000;
    return 0x00040001;
}

// ---------------------------------------------------------------- other dialogs

function openChangeKeyDialog() {
    const password = el("input", { attrs: { type: "password" } });
    const repeat = el("input", { attrs: { type: "password" } });
    const keyFile = el("input", { attrs: { type: "text", placeholder: t("changeKey.optional") } });
    const browse = el("button", { class: "btn", text: t("changeKey.choose") });
    browse.addEventListener("click", async () => {
        const path = await application.openFileDialog(t("lock.chooseKeyFile"), "*.keyx;*", "");
        if (path) keyFile.value = path;
    });
    const ok = el("button", { class: "btn primary", text: t("changeKey.apply") });
    const cancel = el("button", { class: "btn", text: t("dialog.cancel") });
    const dialog = openModal({
        title: t("changeKey.apply"),
        body: el("div", { class: "grid" }, [
            el("label", { text: t("changeKey.newPassword") }), password,
            el("label", { text: t("changeKey.repeat") }), repeat,
            el("label", { text: t("changeKey.keyFile") }), el("div", { class: "row" }, [keyFile, browse]),
            el("div", { class: "full hint", text: t("changeKey.hint") }),
        ]),
        footer: [cancel, ok],
    });
    cancel.addEventListener("click", () => dialog.close());
    ok.addEventListener("click", async () => {
        if (password.value !== repeat.value) {
            toast(t("changeKey.mismatch"), "error");
            return;
        }
        if (!password.value && !keyFile.value.trim()) {
            toast(t("changeKey.needKey"), "error");
            return;
        }
        await vault.changeKey(password.value, keyFile.value.trim());
        dialog.close();
        await refreshAll();
        toast(t("toast.keyChanged"), "success");
    });
}

function openOptionsDialog() {
    const config = Object.assign({}, state.config);
    const clipboard = el("input", { attrs: { type: "number", min: "0", max: "3600", value: String(config.clipboardClearSeconds) } });
    const clipboardExit = el("input", { attrs: { type: "checkbox" } });
    clipboardExit.checked = !!config.clipboardClearOnExit;
    const autoLock = el("input", { attrs: { type: "number", min: "0", max: "86400", value: String(config.autoLockSeconds) } });
    const lockOnMinimize = el("input", { attrs: { type: "checkbox" } });
    lockOnMinimize.checked = !!config.lockOnMinimize;
    const backupCount = el("input", { attrs: { type: "number", min: "0", max: "20", value: String(config.backupCount) } });
    const confirmDelete = el("input", { attrs: { type: "checkbox" } });
    confirmDelete.checked = !!config.confirmDelete;
    const theme = el("select");
    theme.append(el("option", { attrs: { value: "dark" }, text: t("options.themeDark") }));
    theme.append(el("option", { attrs: { value: "light" }, text: t("options.themeLight") }));
    theme.value = config.theme === "light" ? "light" : "dark";
    const languageSelect = el("select");
    for (const entry of SUPPORTED) {
        languageSelect.append(el("option", { attrs: { value: entry.code }, text: entry.label }));
    }
    languageSelect.value = config.language || detectLanguage();
    const configPath = el("div", { class: "hint mono" });
    application.configPath().then((path) => { configPath.textContent = path; });

    const ok = el("button", { class: "btn primary", text: t("menu.save") });
    const cancel = el("button", { class: "btn", text: t("dialog.cancel") });
    const dialog = openModal({
        title: t("options.title"),
        body: el("div", { class: "grid" }, [
            el("label", { text: t("options.clipboard") }), el("div", { class: "row" }, [clipboard, el("span", { class: "hint", text: t("options.seconds") })]),
            el("label", { text: t("options.clipboardExit") }), clipboardExit,
            el("label", { text: t("options.autoLock") }), el("div", { class: "row" }, [autoLock, el("span", { class: "hint", text: t("options.autoLockSeconds") })]),
            el("label", { text: t("options.lockOnMinimize") }), lockOnMinimize,
            el("label", { text: t("options.backupCount") }), backupCount,
            el("label", { text: t("options.confirmDelete") }), confirmDelete,
            el("label", { text: t("options.theme") }), theme,
            el("label", { text: t("options.language") }), languageSelect,
            el("div", { class: "full" }, [configPath]),
        ]),
        footer: [cancel, ok],
    });
    cancel.addEventListener("click", () => dialog.close());
    ok.addEventListener("click", async () => {
        config.clipboardClearSeconds = Number(clipboard.value) || 0;
        config.clipboardClearOnExit = clipboardExit.checked;
        config.autoLockSeconds = Number(autoLock.value) || 0;
        config.lockOnMinimize = lockOnMinimize.checked;
        config.backupCount = Number(backupCount.value) || 0;
        config.confirmDelete = confirmDelete.checked;
        config.theme = theme.value;
        await application.updateConfig(config);
        state.config = config;
        applyTheme(config.theme);
        dialog.close();
        if (languageSelect.value !== state.config.language) {
            // Switching the language re-renders everything, including the native menu.
            await applyLanguage(languageSelect.value);
            toast(t("toast.languageChanged", { language: languageLabel(languageSelect.value) }), "success");
        } else {
            toast(t("toast.optionsSaved"), "success");
        }
    });
}

function openAboutDialog() {
    const body = el("div", {}, [
        el("div", { class: "row" }, [
            el("img", { attrs: { src: "/icon.png", alt: "GoKeep", width: "48", height: "48" } }),
            el("h2", { text: "GoKeep", style: { margin: "0" } }),
        ]),
        el("p", { text: t("about.intro") }),
        el("div", { class: "section-title", text: t("about.formats") }),
        el("ul", {}, [
            el("li", { text: t("about.formatList") }),
            el("li", { text: t("about.formatCipher") }),
            el("li", { text: t("about.formatKdf") }),
            el("li", { text: t("about.formatKey") }),
            el("li", { text: t("about.formatData") }),
        ]),
        el("div", { class: "section-title", text: t("about.compat") }),
        el("p", { text: t("about.compatText") }),
    ]);
    const close = el("button", { class: "btn primary", text: t("dialog.close") });
    const dialog = openModal({ title: t("menu.about"), body, footer: [close] });
    close.addEventListener("click", () => dialog.close());
}

async function showDuplicateDialog() {
    const groups = await vault.duplicates();
    const body = el("div");
    if (groups.length === 0) {
        body.append(el("div", { class: "hint", text: t("duplicates.none") }));
    }
    groups.forEach((group) => {
        body.append(el("div", { class: "section-title", text: t("duplicates.group", { count: group.length }) }));
        const box = el("div", { class: "list-box" });
        for (const entry of group) {
            const open = el("button", { class: "icon-btn", text: t("expired.open") });
            open.addEventListener("click", () => {
                selectGroup(entry.groupId).then(() => selectEntry(entry.id));
                dialog.close();
            });
            box.append(el("div", { class: "item" }, [
                el("span", { class: "grow", text: `${entry.title} — ${entry.groupPath}` }), open,
            ]));
        }
        body.append(box);
    });
    const close = el("button", { class: "btn", text: t("dialog.close") });
    const dialog = openModal({ title: t("duplicates.title"), body, footer: [close] });
    close.addEventListener("click", () => dialog.close());
}

async function showExpiredDialog() {
    const entries = await vault.expired();
    const body = el("div");
    if (entries.length === 0) {
        body.append(el("div", { class: "hint", text: t("expired.none") }));
    } else {
        const box = el("div", { class: "list-box" });
        for (const entry of entries) {
            box.append(el("div", { class: "item" }, [
                el("span", { class: "grow", text: `${entry.title} — ${entry.groupPath}` }),
                el("span", { class: "badge danger", text: formatDate(entry.expires ? entry.modified : "") }),
            ]));
        }
        body.append(box);
    }
    const close = el("button", { class: "btn", text: t("dialog.close") });
    const dialog = openModal({ title: t("expired.title"), body, footer: [close] });
    close.addEventListener("click", () => dialog.close());
}

function openSearchDialog(allGroups) {
    const query = el("input", { attrs: { type: "text", value: state.search } });
    const caseSensitive = el("input", { attrs: { type: "checkbox" } });
    caseSensitive.checked = state.searchOptions.caseSensitive;
    const regex = el("input", { attrs: { type: "checkbox" } });
    regex.checked = state.searchOptions.regex;
    const all = el("input", { attrs: { type: "checkbox" } });
    all.checked = allGroups || state.searchOptions.allGroups;
    const protectedValues = el("input", { attrs: { type: "checkbox" } });
    protectedValues.checked = state.searchOptions.includeProtected;
    const ok = el("button", { class: "btn primary", text: t("search.submit") });
    const cancel = el("button", { class: "btn", text: t("dialog.cancel") });
    const dialog = openModal({
        title: t("search.title"),
        width: "small",
        body: el("div", { class: "grid" }, [
            el("label", { text: t("search.query") }), query,
            el("label", { text: t("search.caseSensitive") }), caseSensitive,
            el("label", { text: t("search.regex") }), regex,
            el("label", { text: t("search.allGroups") }), all,
            el("label", { text: t("search.includeProtected") }), protectedValues,
        ]),
        footer: [cancel, ok],
    });
    cancel.addEventListener("click", () => dialog.close());
    ok.addEventListener("click", async () => {
        state.search = query.value;
        state.searchOptions = {
            caseSensitive: caseSensitive.checked,
            regex: regex.checked,
            allGroups: all.checked,
            includeProtected: protectedValues.checked,
        };
        qs("#search").value = query.value;
        dialog.close();
        await refreshEntries();
    });
}

boot().catch((error) => toast(String(error), "error"));
