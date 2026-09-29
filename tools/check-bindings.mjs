// Fails when api.js calls a service method that is missing from the generated bindings.
// Forgetting to run "wails3 generate bindings" after changing a Go service would
// otherwise surface as "X is not a function" at runtime.
//
//   node tools/check-bindings.mjs

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const api = readFileSync(join(root, "frontend", "dist", "js", "api.js"), "utf8");

const bindingFiles = {
    AppService: "appservice.js",
    VaultService: "vaultservice.js",
    GeneratorService: "generatorservice.js",
};

const sources = {};
for (const [service, file] of Object.entries(bindingFiles)) {
    sources[service] = readFileSync(
        join(root, "frontend", "dist", "bindings", "gokeep", "internal", "app", file),
        "utf8",
    );
}

const used = new Set(
    [...api.matchAll(/(AppService|VaultService|GeneratorService)\.(\w+)/g)]
        .map((match) => `${match[1]}.${match[2]}`),
);

const missing = [...used].filter((entry) => {
    const [service, method] = entry.split(".");
    return !sources[service].includes(`export function ${method}(`);
});

if (missing.length > 0) {
    console.error("missing bindings (run: wails3 generate bindings -b -d frontend/dist/bindings .):");
    for (const entry of missing) {
        console.error(`  ${entry}`);
    }
    process.exit(1);
}
console.log(`all ${used.size} bound methods exist`);

// The generated model for the configuration uses the JSON names (camelCase). Using the
// Go field names instead silently produces undefined values at runtime, so they are
// checked here as well.
const models = readFileSync(
    join(root, "frontend", "dist", "bindings", "gokeep", "internal", "app", "models.js"),
    "utf8",
);
const configClass = models.slice(models.indexOf("export class Config "));
const configFields = new Set(
    [...configClass.matchAll(/this\["(\w+)"\] =/g)].map((match) => match[1]),
);

const appSource = readFileSync(join(root, "frontend", "dist", "js", "app.js"), "utf8");
const usedFields = new Set(
    [...appSource.matchAll(/config\.(\w+)/g)].map((match) => match[1]),
);
const unknownFields = [...usedFields].filter((field) => !configFields.has(field) && field !== "length");

if (unknownFields.length > 0) {
    console.error("unknown configuration fields in the frontend (check the JSON names):");
    for (const field of unknownFields) {
        console.error(`  config.${field}`);
    }
    process.exit(1);
}
console.log(`all ${usedFields.size} configuration fields exist`);
