// Translation support. Language packs live in /locales/<code>.json and are loaded at
// runtime, so a new language is a new file rather than a rebuild.

export const SUPPORTED = [
    { code: "zh-CN", label: "简体中文" },
    { code: "zh-TW", label: "繁體中文" },
    { code: "en", label: "English" },
    { code: "ja", label: "日本語" },
    { code: "ko", label: "한국어" },
];

export const DEFAULT_LANGUAGE = "zh-CN";

let currentLanguage = DEFAULT_LANGUAGE;
let messages = {};
let fallbackMessages = {};
const listeners = new Set();

async function loadPack(code) {
    try {
        const response = await fetch(`/locales/${code}.json`, { cache: "no-store" });
        if (!response.ok) {
            return {};
        }
        return await response.json();
    } catch (error) {
        console.warn(`cannot load language pack ${code}`, error);
        return {};
    }
}

export function normaliseLanguage(code) {
    if (!code) {
        return DEFAULT_LANGUAGE;
    }
    const lower = String(code).toLowerCase();
    if (lower.startsWith("zh")) {
        if (lower.includes("tw") || lower.includes("hk") || lower.includes("hant")) {
            return "zh-TW";
        }
        return "zh-CN";
    }
    if (lower.startsWith("en")) {
        return "en";
    }
    if (lower.startsWith("ja")) {
        return "ja";
    }
    if (lower.startsWith("ko")) {
        return "ko";
    }
    return DEFAULT_LANGUAGE;
}

// detectLanguage guesses the language from the operating system, which WebView2 reports
// through navigator.language ("zh-CN", "ja-JP", ...).
export function detectLanguage() {
    const candidates = [];
    if (typeof navigator !== "undefined") {
        if (Array.isArray(navigator.languages)) {
            candidates.push(...navigator.languages);
        }
        if (navigator.language) {
            candidates.push(navigator.language);
        }
    }
    for (const candidate of candidates) {
        const match = SUPPORTED.find((item) => item.code.toLowerCase() === String(candidate).toLowerCase());
        if (match) {
            return match.code;
        }
        const normalised = normaliseLanguage(candidate);
        if (normalised) {
            return normalised;
        }
    }
    return DEFAULT_LANGUAGE;
}

export async function setLanguage(code, options = {}) {
    const language = normaliseLanguage(code);
    const [pack, fallback] = await Promise.all([
        loadPack(language),
        language === DEFAULT_LANGUAGE ? Promise.resolve(null) : loadPack(DEFAULT_LANGUAGE),
    ]);
    currentLanguage = language;
    messages = pack || {};
    fallbackMessages = fallback || messages;
    if (typeof document !== "undefined") {
        document.documentElement.lang = language;
    }
    if (!options.silent) {
        listeners.forEach((listener) => listener(language));
    }
    return language;
}

export function language() {
    return currentLanguage;
}

export function languageLabel(code = currentLanguage) {
    const entry = SUPPORTED.find((item) => item.code === code);
    return entry ? entry.label : code;
}

export function onLanguageChange(listener) {
    listeners.add(listener);
    return () => listeners.delete(listener);
}

function lookup(source, key) {
    if (!source) {
        return undefined;
    }
    return key.split(".").reduce((value, part) => {
        if (value && typeof value === "object" && part in value) {
            return value[part];
        }
        return undefined;
    }, source);
}

/**
 * t looks up a key and interpolates {placeholders}. Unknown keys fall back to the
 * default language and finally to the key itself, so a missing translation never breaks
 * the user interface.
 */
export function t(key, params = {}) {
    let value = lookup(messages, key);
    if (typeof value !== "string") {
        value = lookup(fallbackMessages, key);
    }
    if (typeof value !== "string") {
        return key;
    }
    return value.replace(/\{(\w+)\}/g, (match, name) => {
        if (params[name] === undefined || params[name] === null) {
            return match;
        }
        return String(params[name]);
    });
}

// section returns a whole group of translated strings, which is what the native menu
// needs: the frontend owns the translations and hands them to the backend.
export function section(name) {
    const value = lookup(messages, name) ?? lookup(fallbackMessages, name);
    if (!value || typeof value !== "object") {
        return {};
    }
    return JSON.parse(JSON.stringify(value));
}

// applyTranslations fills in the static markup: data-i18n for the text content,
// data-i18n-title and data-i18n-placeholder for attributes.
export function applyTranslations(root = document) {
    root.querySelectorAll("[data-i18n]").forEach((node) => {
        node.textContent = t(node.dataset.i18n);
    });
    root.querySelectorAll("[data-i18n-title]").forEach((node) => {
        node.setAttribute("title", t(node.dataset.i18nTitle));
    });
    root.querySelectorAll("[data-i18n-placeholder]").forEach((node) => {
        node.setAttribute("placeholder", t(node.dataset.i18nPlaceholder));
    });
}

// formatDate renders a timestamp in the current language.
export function formatDate(value) {
    if (!value) {
        return "";
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
        return value;
    }
    try {
        return date.toLocaleString(currentLanguage);
    } catch (error) {
        return date.toLocaleString();
    }
}

// formatNumber renders a number in the current language.
export function formatNumber(value) {
    try {
        return Number(value).toLocaleString(currentLanguage);
    } catch (error) {
        return String(value);
    }
}
