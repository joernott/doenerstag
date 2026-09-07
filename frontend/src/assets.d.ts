// Importing an asset gives its text.
//
// The bundler is told to load `.svg` with esbuild's `text` loader (build.mjs)
// and Vitest is told the same thing for the unit tests (vitest.config.ts), so
// an SVG import is the file's markup rather than a URL. That is what lets the
// logo be inlined and take its colour from the page; see src/logo.ts.
declare module "*.svg" {
  const content: string;
  export default content;
}
