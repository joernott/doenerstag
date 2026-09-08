import { readFileSync } from "node:fs";

import { defineConfig } from "vitest/config";

export default defineConfig({
  // An SVG import is the file's markup, not a URL to it.
  //
  // build.mjs tells esbuild the same thing with its `text` loader, and the two
  // have to agree or the logo would be a path in the tests and a drawing in the
  // browser. `enforce: "pre"` puts this ahead of Vite's own asset handling,
  // which would otherwise have turned the import into a URL before this ran.
  plugins: [
    {
      name: "svg-as-text",
      enforce: "pre" as const,
      load(id: string): string | null {
        const path = id.split("?")[0] ?? id;
        return path.endsWith(".svg")
          ? `export default ${JSON.stringify(readFileSync(path, "utf8"))}`
          : null;
      },
    },
  ],
  test: {
    // The frontend is a DOM application: the router listens for clicks, the
    // modal traps focus and the shell writes cookies. Testing any of that
    // against a stub would be testing the stub.
    environment: "jsdom",
    include: ["test/**/*.test.ts"],
    // The formatting tests assert what a date looks like, and a date is only
    // determinate once the time zone is. UTC here, and every helper that must
    // not shift at all -- opening hours, weekday names -- says so itself.
    env: { TZ: "UTC" },
    // The registry module is generated, and a checkout that has not been built
    // does not have it yet. Generating it here means `npx vitest` works on a
    // fresh clone, exactly as `npm run build` does.
    globalSetup: ["./test/global-setup.ts"],
    setupFiles: ["./test/setup.ts"],
  },
});
