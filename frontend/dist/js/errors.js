// Turning arbitrary error payloads into a readable message.
//
// Wails wraps Go errors: what is thrown is a RuntimeError whose "cause" holds whatever
// the backend marshalled. Depending on the backend that can be a string, an object with
// a message field, an empty object, or a nested structure, so every shape is handled
// instead of ending up with "[object Object]".

export function describeError(error) {
    const seen = new Set();
    const walk = (value, depth) => {
        if (value === null || value === undefined || depth > 6) {
            return "";
        }
        if (typeof value === "string") {
            return clean(value);
        }
        if (typeof value === "number" || typeof value === "boolean") {
            return String(value);
        }
        if (value instanceof Error) {
            return clean(value.message || "") || walk(value.cause, depth + 1);
        }
        if (typeof value === "object") {
            if (seen.has(value)) {
                return "";
            }
            seen.add(value);
            for (const key of ["message", "Message", "error", "Error", "detail", "reason"]) {
                if (typeof value[key] === "string" && value[key].trim() !== "") {
                    return clean(value[key]);
                }
            }
            if (Array.isArray(value)) {
                const parts = value.map((item) => walk(item, depth + 1)).filter(Boolean);
                if (parts.length) {
                    return parts.join("; ");
                }
            }
            for (const key of ["cause", "Cause", "inner", "data"]) {
                const nested = walk(value[key], depth + 1);
                if (nested) {
                    return nested;
                }
            }
            try {
                const text = JSON.stringify(value);
                if (text && text !== "{}" && text !== "null") {
                    return clean(text);
                }
            } catch (ignored) {
                /* circular structures cannot be stringified */
            }
        }
        return "";
    };
    return walk(error, 0) || "操作失败：后端没有返回可读的错误信息";
}

// errorCode extracts the machine readable code the backend attaches to an error.
export function errorCode(error) {
    let current = error;
    for (let depth = 0; current && depth < 6; depth++) {
        if (typeof current === "object") {
            if (typeof current.code === "string") {
                return current.code;
            }
            if (current.cause && current.cause !== current) {
                current = current.cause;
                continue;
            }
        }
        break;
    }
    return "";
}

function clean(text) {
    return String(text).replace(/^Error:\s*/, "").trim();
}
