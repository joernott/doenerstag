/**
 * The list of accounts, for the places that name one.
 *
 * An order says who collects the money and who fetches the food, and both used
 * to be typed. A typed name is not a person: it cannot be compared with the
 * person looking at the page, so the order page could not tell whether "Jo" was
 * the Jo pressing the button, and two colleagues who spell each other's names
 * differently are two different collectors.
 *
 * GET /users returns the public profile of every account to any signed-in
 * caller, so this is a list of ids with something to show for each.
 */

import { getList } from "./api";

/** An account, as little of one as naming it needs. */
export interface Account {
  id: string;
  name: string;
  display_name: string;
}

/** What to show for an account: its display name, or its user name. */
export function accountLabel(account: Account): string {
  return account.display_name.trim() || account.name;
}

/**
 * Every account, ordered by what they are called.
 *
 * Sorted here rather than by the API: the order wanted is the reader's
 * alphabet, and the server does not know which one that is. `undefined` uses
 * the browser's own locale, which is the reader's.
 */
export async function listAccounts(): Promise<Account[]> {
  const accounts = await getList<Account>("/users", "users");
  return accounts.sort((a, b) => accountLabel(a).localeCompare(accountLabel(b)));
}

/**
 * The choices for a field that names an account, "nobody" first.
 *
 * Nobody is a real answer and the one an order starts with, so it is an option
 * rather than an empty state: a select whose first entry is blank looks like a
 * list that failed to load.
 */
export function accountChoices(
  accounts: Account[],
  nobody: string,
): { value: string; label: string }[] {
  return [
    { value: "", label: nobody },
    ...accounts.map((account) => ({ value: account.id, label: accountLabel(account) })),
  ];
}
