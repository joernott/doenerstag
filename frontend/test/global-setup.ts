// Generates the catalog registry before the suite runs.
//
// The registry is a build artefact and is not committed, so a fresh clone has
// no src/i18n/registry.generated.ts and every test importing the i18n runtime
// would fail to resolve it. Generating it here keeps `npx vitest` working
// without a build step, and uses the same function the build does.
//
// Not strict: a catalog problem must be reported by the catalog test, which
// says which key is missing, rather than by a setup failure that says only
// that setup failed.

import { generateRegistry } from "../scripts/i18n.mjs";

export default async function setup(): Promise<void> {
  await generateRegistry({ strict: false });
}
