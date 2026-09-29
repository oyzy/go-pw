// Verifies that every language pack has exactly the same keys as the base language
// (zh-CN) and that placeholders such as {count} match.
//
//   node tools/i18n-check.mjs

import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const localeDir = join(root, "frontend", "dist", "locales");
const base = "zh-CN";

function flatten(source, prefix = "", out = {}) {
    for (const [key, value] of Object.entries(source)) {
        const path = prefix ? `${prefix}.${key}` : key;
        if (value && typeof value === "object") {
            flatten(value, path, out);
        } else {
            out[path] = String(value);
        }
    }
    return out;
}

function placeholders(text) {
    return [...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort().join(",");
}

const baseKeys = flatten(JSON.parse(readFileSync(join(localeDir, `${base}.json`), "utf8")));
let failures = 0;

for (const file of readdirSync(localeDir).filter((name) => name.endsWith(".json"))) {
    const code = file.replace(/\.json$/, "");
    const keys = flatten(JSON.parse(readFileSync(join(localeDir, file), "utf8")));
    const missing = Object.keys(baseKeys).filter((key) => !(key in keys));
    const extra = Object.keys(keys).filter((key) => !(key in baseKeys));
    const mismatched = Object.keys(baseKeys).filter((key) =>
        key in keys && placeholders(baseKeys[key]) !== placeholders(keys[key]));

    const status = missing.length === 0 && extra.length === 0 && mismatched.length === 0 ? "ok  " : "FAIL";
    console.log(`${status} ${code.padEnd(6)} keys=${String(Object.keys(keys).length).padStart(4)}`);
    if (missing.length) {
        console.log(`     missing: ${missing.slice(0, 10).join(", ")}`);
        failures++;
    }
    if (extra.length) {
        console.log(`     extra: ${extra.slice(0, 10).join(", ")}`);
        failures++;
    }
    if (mismatched.length) {
        console.log(`     placeholder mismatch: ${mismatched.slice(0, 10).join(", ")}`);
        failures++;
    }
}

console.log(`base keys: ${Object.keys(baseKeys).length}`);
process.exit(failures === 0 ? 0 : 1);
