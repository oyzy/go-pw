// One-off migration helper: replaces Chinese string literals in the frontend with t()
// lookups against the language packs. Run with --apply to rewrite the files, without it
// only reports what would change.
//
//   node tools/i18n-migrate.mjs            # report unmapped literals
//   node tools/i18n-migrate.mjs --apply    # rewrite the frontend files
//
// It is kept in the repository because it doubles as a checker: any Chinese literal that
// is not backed by a key in locales/zh-CN.json is reported.

import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const apply = process.argv.includes("--apply");
const files = [
    join(root, "frontend", "dist", "js", "app.js"),
    join(root, "frontend", "dist", "js", "ui.js"),
];

function flatten(source, prefix = "", out = new Map()) {
    for (const [key, value] of Object.entries(source)) {
        const path = prefix ? `${prefix}.${key}` : key;
        if (value && typeof value === "object") {
            flatten(value, path, out);
        } else if (typeof value === "string") {
            // Placeholders are compared without their names, so that a template literal
            // in the code (${state.info.name}) still matches "{name}" in the pack.
            out.set(placeholderShape(value), { key: path, names: placeholderNames(value) });
        }
    }
    return out;
}

function placeholderNames(text) {
    return [...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]);
}

function placeholderShape(text) {
    return text.replace(/\{\w+\}/g, "{}");
}

const pack = JSON.parse(readFileSync(join(root, "frontend", "dist", "locales", "zh-CN.json"), "utf8"));
const byValue = flatten(pack);

// Strings that must stay as they are even though they contain Chinese characters.
const skipValues = new Set(["回收站"]);

const literalPattern = /"((?:[^"\\]|\\.)*)"|`((?:[^`\\]|\\.)*)`/g;
const han = /[\u4e00-\u9fff]/;

const unmapped = new Map();
let replaced = 0;

for (const file of files) {
    const original = readFileSync(file, "utf8");
    const lines = original.split(/\r?\n/);
    const output = lines.map((line) => {
        if (line.trimStart().startsWith("//") || line.trimStart().startsWith("*")) {
            return line;
        }
        return line.replace(literalPattern, (match, quoted, templated) => {
            const isTemplate = templated !== undefined;
            let text = isTemplate ? templated : quoted;
            if (!isTemplate) {
                // Decode the escapes so the literal can be compared with the pack value.
                try {
                    text = JSON.parse(match);
                } catch (error) {
                    return match;
                }
            }
            if (!text || !han.test(text) || skipValues.has(text)) {
                return match;
            }
            const expressions = [...text.matchAll(/\$\{([^}]+)\}/g)].map((match) => match[1].trim());
            const shape = text.replace(/\$\{[^}]+\}/g, "{}");
            const entry = byValue.get(shape);
            if (!entry || entry.names.length !== expressions.length) {
                unmapped.set(shape, (unmapped.get(shape) || 0) + 1);
                return match;
            }
            replaced++;
            if (expressions.length === 0) {
                return `t("${entry.key}")`;
            }
            const params = entry.names.map((name, index) => `${name}: ${expressions[index]}`).join(", ");
            return `t("${entry.key}", { ${params} })`;
        });
    });
    if (apply) {
        writeFileSync(file, output.join("\n"), "utf8");
    }
}

console.log(`${apply ? "replaced" : "would replace"}: ${replaced} literals`);
if (unmapped.size > 0) {
    console.log(`unmapped literals (${unmapped.size}):`);
    for (const [text, count] of unmapped) {
        console.log(`  ${count}x ${JSON.stringify(text)}`);
    }
}
