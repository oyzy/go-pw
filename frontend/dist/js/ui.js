// Small DOM helpers, toast notifications and the modal dialog framework.

import { t } from "./i18n.js";

export function el(tag, options = {}, children = []) {
    const node = document.createElement(tag);
    if (options.class) node.className = options.class;
    if (options.text !== undefined) node.textContent = options.text;
    if (options.html !== undefined) node.innerHTML = options.html;
    if (options.attrs) {
        for (const [key, value] of Object.entries(options.attrs)) {
            if (value !== null && value !== undefined) node.setAttribute(key, value);
        }
    }
    if (options.style) Object.assign(node.style, options.style);
    if (options.on) {
        for (const [event, handler] of Object.entries(options.on)) {
            node.addEventListener(event, handler);
        }
    }
    for (const child of [].concat(children)) {
        if (child === null || child === undefined || child === false) continue;
        node.append(child instanceof Node ? child : document.createTextNode(String(child)));
    }
    return node;
}

export function clear(node) {
    while (node.firstChild) node.removeChild(node.firstChild);
}

export function qs(selector) {
    return document.querySelector(selector);
}

const toastIcons = {
    error: "!",
    success: "✓",
    warning: "!",
    info: "i",
};

/**
 * toast shows a notification. Errors stay longer, are announced more loudly and can be
 * dismissed by clicking them.
 */
export function toast(message, kind = "info", options = {}) {
    const { title = "", timeout = null } = options;
    const container = qs("#toasts");
    const seconds = timeout !== null
        ? timeout
        : (kind === "error" ? 10000 : kind === "success" ? 4000 : 6000);
    const close = el("button", { class: "toast-close", text: "✕", attrs: { title: t("dialog.close") } });
    const node = el("div", { class: `toast ${kind}`, attrs: { role: kind === "error" ? "alert" : "status" } }, [
        el("div", { class: "toast-icon", text: toastIcons[kind] || "i" }),
        el("div", { class: "toast-body" }, [
            el("div", { class: "toast-title", text: title || t(`toast.title.${kind}`) }),
            el("div", { class: "toast-message", text: String(message) }),
        ]),
        close,
    ]);
    container.append(node);

    let timer = null;
    const dismiss = () => {
        if (timer) clearTimeout(timer);
        node.classList.add("leaving");
        setTimeout(() => node.remove(), 200);
    };
    close.addEventListener("click", dismiss);
    node.addEventListener("click", (event) => {
        if (event.target !== close) dismiss();
    });
    timer = setTimeout(dismiss, seconds);
    return dismiss;
}

/**
 * openModal builds a dialog and returns handles to close it or to read its state.
 */
export function openModal({ title, body, footer, width = "", onClose = null, onKeyDown = null }) {
    const backdrop = el("div", { class: "modal-backdrop" });
    const modal = el("div", { class: `modal ${width}`.trim() });
    const head = el("div", { class: "modal-head" }, [el("div", { text: title })]);
    const closeButton = el("button", { class: "icon-btn", text: "✕", attrs: { title: t("dialog.close") } });
    head.append(el("div", { style: { flex: "1" } }), closeButton);
    const bodyNode = el("div", { class: "modal-body" }, [].concat(body));
    modal.append(head, bodyNode);
    if (footer) {
        modal.append(el("div", { class: "modal-foot" }, [].concat(footer)));
    }
    backdrop.append(modal);
    qs("#modals").append(backdrop);

    const handle = {
        root: modal,
        body: bodyNode,
        closed: false,
        // close takes the dialog away without reporting a cancellation: the button handler
        // that has already decided the outcome calls it before resolving its promise.
        close() {
            if (handle.closed) return;
            handle.closed = true;
            backdrop.remove();
            document.removeEventListener("keydown", keyHandler, true);
        },
        // dismiss is what the ✕ button, the Escape key and a click on the backdrop do; it
        // counts as "the user cancelled" and reports that through onClose.
        dismiss() {
            if (handle.closed) return;
            handle.close();
            if (onClose) onClose();
        },
    };
    const keyHandler = (event) => {
        if (event.key === "Escape") {
            event.preventDefault();
            handle.dismiss();
            return;
        }
        if (onKeyDown) onKeyDown(event, handle);
    };
    closeButton.addEventListener("click", () => handle.dismiss());
    backdrop.addEventListener("mousedown", (event) => {
        if (event.target === backdrop && !modal.dataset.sticky) handle.dismiss();
    });
    document.addEventListener("keydown", keyHandler, true);
    return handle;
}

/**
 * prompt shows a small input dialog and resolves with the entered text or null.
 */
export function prompt({ title, label, value = "", type = "text", okLabel = "" }) {
    return new Promise((resolve) => {
        const input = el("input", { attrs: { type, value } });
        input.style.width = "100%";
        let handle;
        const ok = el("button", { class: "btn primary", text: okLabel || t("dialog.ok") });
        const cancel = el("button", { class: "btn", text: t("dialog.cancel") });
        const submit = () => {
            const text = input.value;
            handle.close();
            resolve(text);
        };
        ok.addEventListener("click", submit);
        cancel.addEventListener("click", () => {
            handle.close();
            resolve(null);
        });
        handle = openModal({
            title,
            width: "small",
            body: [
                el("label", { class: "hint", text: label }),
                el("div", { style: { marginTop: "8px" } }, [input]),
            ],
            footer: [cancel, ok],
            onClose: () => resolve(null),
            onKeyDown: (event) => {
                if (event.key === "Enter") {
                    event.preventDefault();
                    submit();
                }
            },
        });
        setTimeout(() => {
            input.focus();
            input.select();
        }, 30);
    });
}

/**
 * confirmDialog asks a yes/no question with a promise based API.
 */
export function confirmDialog({ title, message, okLabel = "", danger = false }) {
    return new Promise((resolve) => {
        let handle;
        const ok = el("button", { class: `btn ${danger ? "danger" : "primary"}`, text: okLabel || t("dialog.ok") });
        const cancel = el("button", { class: "btn", text: t("dialog.cancel") });
        ok.addEventListener("click", () => {
            handle.close();
            resolve(true);
        });
        cancel.addEventListener("click", () => {
            handle.close();
            resolve(false);
        });
        handle = openModal({
            title,
            width: "small",
            body: el("div", { text: message }),
            footer: [cancel, ok],
            onClose: () => resolve(false),
        });
        setTimeout(() => ok.focus(), 30);
    });
}

export function formatDate(value) {
    if (!value) return "";
    const date = new Date(value);
    if (isNaN(date.getTime())) return value;
    return date.toLocaleString();
}

export function qualityBadge(score) {
    let kind = "danger";
    if (score >= 80) kind = "ok";
    else if (score >= 55) kind = "ok";
    else if (score >= 35) kind = "warn";
    return el("span", { class: `badge ${kind}`, text: `${score}` });
}

export function iconForEntry() {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.innerHTML = '<path d="M15 7a5 5 0 1 0-4.6 5H13l2 2 2-2 2 2v-3h-3z"/>';
    return svg;
}
