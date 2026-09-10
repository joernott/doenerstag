// The account list, which is what lets an order name a person rather than
// describe one.

import { describe, expect, it } from "vitest";

import { accountChoices, accountLabel, DELETED_USER_ID } from "../src/accounts";

const accounts = [
  { id: "u1", name: "jo", display_name: "Jo Ott" },
  { id: "u2", name: "alex", display_name: "" },
  { id: DELETED_USER_ID, name: "deleted", display_name: "deleted user" },
];

describe("what an account is called", () => {
  it("is the display name when there is one", () => {
    expect(accountLabel(accounts[0]!)).toBe("Jo Ott");
  });

  it("falls back to the user name", () => {
    expect(accountLabel(accounts[1]!)).toBe("alex");
  });

  it("falls back when the display name is only spaces", () => {
    expect(accountLabel({ id: "u3", name: "sam", display_name: "   " })).toBe("sam");
  });
});

describe("the choices for a field naming an account", () => {
  it("offers nobody first, because that is where an order starts", () => {
    const choices = accountChoices(accounts, "Nobody yet");
    expect(choices[0]).toEqual({ value: "", label: "Nobody yet" });
  });

  /*
   * The placeholder is a row in app_user and not a person: it owns whatever a
   * deleted account left behind, so that an old order still reads "1x Döner,
   * no onions" without naming anyone. Offering it as somebody who might fetch
   * the food is nonsense, and the server refuses it too.
   */
  it("leaves out the deleted-user placeholder", () => {
    const choices = accountChoices(accounts, "Nobody yet");
    expect(choices.map((c) => c.value)).toEqual(["", "u1", "u2"]);
  });
});
