// Copying text, where the browser lets a page do that.

/**
 * Copies text to the clipboard, resolving to whether it worked.
 *
 * The clipboard API is absent outside a secure context -- a development server
 * with a self-signed certificate is one such place -- and a browser may refuse
 * permission. Either way the caller gets false and offers the text some other
 * way, rather than a button that does nothing. The summary and the API token
 * dialog both copy through this.
 */
export async function copyText(text: string): Promise<boolean> {
  if (!(navigator as Partial<Navigator>).clipboard) {
    return false;
  }
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
