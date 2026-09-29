// Thin wrapper around the generated Wails bindings: every call reports failures as
// toasts instead of unhandled promise rejections.

import { AppService, VaultService, GeneratorService, Config } from "../bindings/gokeep/internal/app/index.js";
import { describeError, errorCode } from "./errors.js";

export { AppService, VaultService, GeneratorService, Config };
export { describeError, errorCode };

let toastHandler = () => {};

export function onError(handler) {
    toastHandler = handler;
}

/**
 * call unwraps a Wails service call: null results and Go errors become JavaScript
 * exceptions that are reported to the user.
 */
export async function call(promise, options = {}) {
    try {
        const result = await promise;
        return result;
    } catch (error) {
        const message = describeError(error);
        const wrapped = new Error(message);
        wrapped.code = errorCode(error);
        wrapped.cause = error;
        if (options.silent !== true) {
            toastHandler(message, "error", { title: options.title, timeout: options.timeout });
        }
        throw wrapped;
    }
}


// Convenience wrappers, grouped by service.
export const vault = {
    open: (path, password, keyFile) => call(VaultService.OpenDatabase(path, password, keyFile)),
    create: (path, name, password, keyFile, settings) =>
        call(VaultService.CreateDatabase(path, name, password, keyFile, settings)),
    close: (saveFirst) => call(VaultService.CloseDatabase(saveFirst)),
    save: () => call(VaultService.SaveDatabase()),
    saveAs: (path) => call(VaultService.SaveDatabaseAs(path)),
    lock: () => call(VaultService.LockDatabase()),
    changeKey: (password, keyFile) => call(VaultService.ChangeMasterKey(password, keyFile)),
    createKeyFile: (path) => call(VaultService.CreateKeyFile(path)),
    info: () => call(VaultService.Info()),
    isOpen: () => call(VaultService.IsOpen(), { silent: true }).catch(() => false),
    tree: () => call(VaultService.Tree()),
    groups: (includeRecycled) => call(VaultService.Groups(includeRecycled)),
    rootId: () => call(VaultService.RootID()),
    entries: (groupId, recursive, includeRecycled) =>
        call(VaultService.Entries(groupId, recursive, includeRecycled)),
    allEntries: (includeRecycled) => call(VaultService.AllEntries(includeRecycled)),
    entry: (id) => call(VaultService.Entry(id)),
    createEntry: (groupId, dto) => call(VaultService.CreateEntry(groupId, dto)),
    updateEntry: (dto) => call(VaultService.UpdateEntry(dto)),
    deleteEntry: (id, permanently) => call(VaultService.DeleteEntry(id, permanently)),
    restoreEntry: (id, groupId) => call(VaultService.RestoreEntry(id, groupId)),
    restoreToPreviousGroup: (id) => call(VaultService.RestoreToPreviousGroup(id)),
    moveEntry: (id, groupId) => call(VaultService.MoveEntry(id, groupId)),
    duplicateEntry: (id) => call(VaultService.DuplicateEntry(id)),
    createGroup: (parentId, name, iconId) => call(VaultService.CreateGroup(parentId, name, iconId)),
    updateGroup: (dto) => call(VaultService.UpdateGroup(dto)),
    deleteGroup: (id, permanently) => call(VaultService.DeleteGroup(id, permanently)),
    moveGroup: (id, parentId) => call(VaultService.MoveGroup(id, parentId)),
    emptyRecycleBin: () => call(VaultService.EmptyRecycleBin()),
    restoreHistory: (id, index) => call(VaultService.RestoreHistory(id, index)),
    deleteHistory: (id, index) => call(VaultService.DeleteHistory(id, index)),
    addAttachment: (id, name, data) => call(VaultService.AddAttachment(id, name, data)),
    removeAttachment: (id, name) => call(VaultService.RemoveAttachment(id, name)),
    exportAttachment: (id, name, path) => call(VaultService.ExportAttachment(id, name, path)),
    search: (options) => call(VaultService.Search(options)),
    duplicates: () => call(VaultService.FindDuplicates()),
    expired: () => call(VaultService.ExpiredEntries()),
    tags: () => call(VaultService.TagList()),
    settings: () => call(VaultService.DatabaseSettings()),
    applySettings: (settings) => call(VaultService.ApplyDatabaseSettings(settings)),
    touch: (id) => call(VaultService.TouchEntryAccess(id), { silent: true }).catch(() => {}),
    copyField: (id, field) => call(VaultService.CopyField(id, field)),
    clearClipboard: () => call(VaultService.ClearClipboard()),
    clipboardStatus: () => call(VaultService.ClipboardStatus(), { silent: true }).catch(() => [false, 0]),
    evaluate: (password) => call(VaultService.EvaluatePassword(password), { silent: true }),
    exportCsv: (path) => call(VaultService.ExportCSV(path)),
    importCsv: (path, groupId) => call(VaultService.ImportCSV(path, groupId)),
    backup: (target) => call(VaultService.CopyDatabaseFile(target)),
};

export const generator = {
    generate: (options) => call(GeneratorService.Generate(options)),
    generateAndStore: (entryId, options, field) => call(GeneratorService.GenerateAndStore(entryId, options, field)),
    evaluate: (password) => call(GeneratorService.Evaluate(password), { silent: true }),
    defaults: () => call(GeneratorService.Defaults(), { silent: true }),
    saveDefaults: (options) => call(GeneratorService.SaveDefaults(options), { silent: true }),
    validatePattern: (pattern) => call(GeneratorService.ValidatePattern(pattern)),
};

export const application = {
    config: () => call(AppService.Config()),
    updateConfig: (config) => call(AppService.UpdateConfig(config)),
    configPath: () => call(AppService.ConfigPath(), { silent: true }),
    version: () => call(AppService.Version(), { silent: true }),
    platform: () => call(AppService.Platform(), { silent: true }),
    recentFiles: () => call(AppService.RecentFiles(), { silent: true }).catch(() => []),
    removeRecent: (path) => call(AppService.RemoveRecent(path), { silent: true }),
    clearRecent: () => call(AppService.ClearRecent(), { silent: true }),
    openFileDialog: (title, pattern, directory) =>
        call(AppService.OpenFileDialog(title, pattern, directory)),
    saveFileDialog: (title, defaultName, pattern) =>
        call(AppService.SaveFileDialog(title, defaultName, pattern)),
    openFolderDialog: (title) => call(AppService.OpenFolderDialog(title)),
    infoDialog: (title, message) => call(AppService.InfoDialog(title, message), { silent: true }),
    questionDialog: (title, message) => call(AppService.QuestionDialog(title, message)),
    readFile: (path) => call(AppService.ReadFileBase64(path)),
    writeFile: (path, data) => call(AppService.WriteFileBase64(path, data)),
    defaultPath: (name) => call(AppService.DefaultDatabasePath(name), { silent: true }),
    quit: (saveFirst) => call(AppService.Quit(saveFirst)),
    minimize: () => call(AppService.Minimize(), { silent: true }),
    iconNames: () => call(AppService.IconNames(), { silent: true }).catch(() => ({})),
    presets: () => call(AppService.VaultSettingsPresets(), { silent: true }).catch(() => ({})),
    openExternal: (target) => call(AppService.OpenExternal(target)),
    setTitle: (title) => call(AppService.SetWindowTitle(title), { silent: true }),
    applyLanguage: (language, labels) => call(AppService.ApplyLanguage(language, labels), { silent: true }),
    applyTheme: (theme) => call(AppService.ApplyTheme(theme), { silent: true }),
};
