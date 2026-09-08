// eslint, in its flat configuration.
//
// docs/08_technologies.md and docs/11_nonfunctional.md both say the TypeScript
// compiles under `strict` and passes eslint. The compiler covers types; this
// covers what a type-correct program can still get wrong -- an unawaited
// promise, a floating `any`, a comparison that is always true.
//
// Type-aware rules are on. They need the compiler to run, which makes linting
// slower than the syntactic rules alone, but the rules worth having here --
// no-floating-promises above all, in a codebase whose every render is async --
// cannot be checked without types.

import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: ["node_modules/**", "src/i18n/registry.generated.ts"],
  },

  // The build scripts: plain JavaScript, running in Node.
  {
    files: ["*.mjs", "scripts/**/*.mjs"],
    ...js.configs.recommended,
    languageOptions: {
      ecmaVersion: 2023,
      sourceType: "module",
      globals: globals.node,
    },
  },

  // The application and its tests.
  {
    files: ["src/**/*.ts", "test/**/*.ts", "vitest.config.ts"],
    extends: [js.configs.recommended, ...tseslint.configs.recommendedTypeChecked],
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
      globals: { ...globals.browser, ...globals.node },
    },
    rules: {
      // An unused argument named with a leading underscore is a documented
      // gap in a signature somebody else defined, not a mistake.
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
      ],
    },
  },
);
