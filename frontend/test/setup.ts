// Per-file test setup.
//
// jsdom has no layout and therefore no scrolling: window.scrollTo reports
// itself as not implemented on every call, which fills the output with stack
// traces from code that is behaving correctly. Replacing it says the same
// thing -- there is nothing to scroll -- without the noise.

window.scrollTo = (): void => {};
