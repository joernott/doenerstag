// Builds the doenerstag frontend into ../static.
//
// Three outputs, all of them checked in so that a release can be built without
// the Node toolchain (docs/08_technologies.md):
//
//   static/index.html   the application shell
//   static/js/app.js    the esbuild bundle
//   static/css/app.css  the compiled Tailwind stylesheet
//   static/img/         the mark, served as the favicon
//   static/swagger/     the vendored Swagger UI, served at /tools/swagger
//
// Nothing here reaches the network at runtime. Every asset is copied from a
// local dependency, because the application must run without internet access
// and the CSP forbids a CDN.

import { spawn } from "node:child_process";
import { watch as watchDirectory } from "node:fs";
import { cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import * as esbuild from "esbuild";

import { catalogDir, generateRegistry } from "./scripts/i18n.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, "..", "static");
const watch = process.argv.includes("--watch");

/** Runs a command and rejects if it fails, so a broken build stops the build. */
function run(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: here,
      stdio: "inherit",
      shell: process.platform === "win32",
      ...options,
    });
    child.on("error", reject);
    child.on("exit", (code) =>
      code === 0 ? resolve() : reject(new Error(`${command} exited with ${code}`)),
    );
  });
}

async function buildScripts() {
  const options = {
    entryPoints: [join(here, "src", "main.ts")],
    bundle: true,
    format: "esm",
    target: ["es2022"],
    outfile: join(out, "js", "app.js"),
    // Sourcemaps only outside a release: they would otherwise ship the
    // TypeScript sources to every visitor.
    sourcemap: watch ? "inline" : false,
    minify: !watch,
    // The logo is imported as text and parsed into the DOM at runtime, which is
    // what lets one file serve both themes: `currentColor` needs the mark to be
    // part of the page, and an <img> is a separate document that inherits no
    // colour from it. See src/logo.ts.
    loader: { ".svg": "text" },
    logLevel: "info",
  };

  if (!watch) {
    await esbuild.build(options);
    return;
  }
  const context = await esbuild.context(options);
  await context.watch();
}

async function buildStyles() {
  const args = [
    "@tailwindcss/cli",
    "--input", join(here, "styles", "app.css"),
    "--output", join(out, "css", "app.css"),
  ];
  if (watch) {
    args.push("--watch");
  } else {
    args.push("--minify");
  }
  await run("npx", args);
}

/**
 * Writes the application shell.
 *
 * The theme attribute is set before first paint from the doener_theme cookie,
 * so a light-mode user does not see a dark flash on every navigation. This is
 * the one script in the document, and it is a file rather than an inline block
 * because the CSP forbids inline scripts.
 */
async function buildShell() {
  const html = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>doenerstag</title>
    <!-- The mark, as the tab icon. Firefox asks for /favicon.ico on every page
         whether or not one is declared; naming a real one here means it asks
         for something that exists. -->
    <link rel="icon" href="/static/img/doenerstag.svg" type="image/svg+xml">
    <link rel="stylesheet" href="/static/css/app.css">
    <script type="module" src="/static/js/theme.js"></script>
  </head>
  <body>
    <noscript>doenerstag needs JavaScript.</noscript>
    <!-- The application renders its whole chrome into this element. The lang
         attribute above is corrected to the resolved interface language as
         soon as the bundle runs. -->
    <div id="app"></div>
    <script type="module" src="/static/js/app.js"></script>
  </body>
</html>
`;
  await writeFile(join(out, "index.html"), html, "utf8");

  const theme = `// Applies the stored theme before first paint, so switching pages does not
// flash the wrong one. Kept out of the bundle because it must run first.
const match = document.cookie.match(/(?:^|;\\s*)doener_theme=(dark|light)/);
if (match) {
  document.documentElement.dataset.theme = match[1];
}
`;
  await writeFile(join(out, "js", "theme.js"), theme, "utf8");

  // The mark as a file as well as inside the bundle. The bundle inlines it so
  // that `currentColor` can follow the theme; a favicon is a separate document
  // and needs a URL of its own.
  await mkdir(join(out, "img"), { recursive: true });
  await cp(join(here, "assets", "doenerstag.svg"), join(out, "img", "doenerstag.svg"));
}

/**
 * Vendors Swagger UI.
 *
 * Only the files the page actually loads are copied: the bundle, the preset,
 * the stylesheet and the favicons. The source maps are several megabytes and
 * nothing serves them.
 */
async function buildSwagger() {
  const dist = dirname(
    fileURLToPath(await import.meta.resolve("swagger-ui-dist/package.json")),
  );
  const target = join(out, "swagger");
  await rm(target, { recursive: true, force: true });
  await mkdir(target, { recursive: true });

  const wanted = [
    "swagger-ui.css",
    "swagger-ui-bundle.js",
    "swagger-ui-standalone-preset.js",
    "favicon-16x16.png",
    "favicon-32x32.png",
  ];
  for (const name of wanted) {
    await cp(join(dist, name), join(target, name));
  }

  // Our own page rather than the packaged index.html: that one points at
  // petstore.swagger.io, which would both fail offline and be wrong.
  const html = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>doenerstag API</title>
    <link rel="icon" href="/static/swagger/favicon-32x32.png" sizes="32x32">
    <link rel="stylesheet" href="/static/swagger/swagger-ui.css">
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="/static/swagger/swagger-ui-bundle.js"></script>
    <script src="/static/swagger/init.js"></script>
  </body>
</html>
`;
  await writeFile(join(target, "index.html"), html, "utf8");

  const init = `// Separate file rather than an inline block: the CSP forbids inline scripts,
// and Swagger UI is served under the same policy as everything else.
window.SwaggerUIBundle({
  url: "/api/v1/openapi.json",
  dom_id: "#swagger-ui",
  deepLinking: true,
  // No "try it out" against another server: the document describes this one.
  supportedSubmitMethods: ["get"],
});
`;
  await writeFile(join(target, "init.js"), init, "utf8");
}

/**
 * Watches the catalog directory for languages being added or removed.
 *
 * esbuild already rebuilds when a catalog's *contents* change, because the
 * registry imports the JSON files. What it cannot notice is a new file
 * appearing, since nothing imports it yet -- so dropping fr.json into the
 * directory during a watch session would otherwise do nothing at all.
 */
function watchCatalogs() {
  let pending = null;
  watchDirectory(catalogDir, (_event, filename) => {
    if (!filename || !filename.endsWith(".json")) {
      return;
    }
    // Editors write in several steps, so a save can fire three events. One
    // regeneration a beat later is enough for all of them.
    clearTimeout(pending);
    pending = setTimeout(() => {
      generateRegistry({ strict: false }).catch((error) => console.error(error.message));
    }, 50);
  });
}

async function main() {
  await mkdir(join(out, "js"), { recursive: true });
  await mkdir(join(out, "css"), { recursive: true });

  // First, because the bundle imports what it writes. A missing key fails the
  // build here rather than showing up as a raw message key on a screen.
  const catalogs = await generateRegistry({ strict: !watch });

  await buildShell();
  await buildSwagger();
  await Promise.all([buildScripts(), buildStyles()]);

  if (watch) {
    watchCatalogs();
    return;
  }

  const bundle = await readFile(join(out, "js", "app.js"), "utf8");
  const codes = catalogs.map((catalog) => catalog.code).join(", ");
  console.log(`built static/js/app.js (${bundle.length} bytes, languages: ${codes})`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
