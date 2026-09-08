// Applies the stored theme before first paint, so switching pages does not
// flash the wrong one. Kept out of the bundle because it must run first.
const match = document.cookie.match(/(?:^|;\s*)doener_theme=(dark|light)/);
if (match) {
  document.documentElement.dataset.theme = match[1];
}
