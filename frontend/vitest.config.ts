import { defineConfig } from "vitest/config";

export default defineConfig({
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
