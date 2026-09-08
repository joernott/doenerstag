// The password complexity rules.
//
// These mirror internal/auth/complexity_test.go on purpose. Two
// implementations of one rule is a thing to keep honest, and the way to keep it
// honest is for both test suites to assert the same cases -- including the ones
// that are easy to get wrong: NFC, the space that earns nothing, and a
// passphrase that passes on fewer classes than it looks like it should.

import { describe, expect, it } from "vitest";

import { checkPassword, passwordLength, satisfiedClasses } from "../src/password";

describe("character classes", () => {
  it("recognises each one", () => {
    expect(satisfiedClasses("A")).toEqual(["upper"]);
    expect(satisfiedClasses("a")).toEqual(["lower"]);
    expect(satisfiedClasses("1")).toEqual(["digit"]);
    expect(satisfiedClasses("!")).toEqual(["special"]);
    expect(satisfiedClasses("ä")).toEqual(["other"]);
    expect(satisfiedClasses("€")).toEqual(["other"]);
  });

  it("lists them in the order the specification gives", () => {
    expect(satisfiedClasses("1aA")).toEqual(["upper", "lower", "digit"]);
  });

  it("gives a plain space no class of its own", () => {
    // "hello world" must not pass a class it has not earned.
    expect(satisfiedClasses("hello world")).toEqual(["lower"]);
  });

  it("treats a decomposed character as the character it is", () => {
    // "a" plus a combining diaeresis, which is what a Mac keyboard produces.
    // Without normalisation the "a" would count as a lower-case letter and the
    // mark as nothing, so one password typed on two machines would be judged
    // differently.
    const decomposed = "ä";
    expect(decomposed.normalize("NFC")).toHaveLength(1);
    expect(satisfiedClasses(decomposed)).toEqual(["other"]);
    expect(passwordLength(decomposed)).toBe(1);
  });
});

describe("what the server will accept", () => {
  it("wants ten characters", () => {
    expect(checkPassword("Ab1!x").acceptable).toBe(false);
    expect(checkPassword("Abcdefgh1!").acceptable).toBe(true);
  });

  it("wants three of the five classes", () => {
    expect(checkPassword("abcdefghijkl").acceptable).toBe(false);
    expect(checkPassword("abcdefghijK").acceptable).toBe(false);
    expect(checkPassword("abcdefghij1K").acceptable).toBe(true);
  });

  it("lets a passphrase through on the classes it actually has", () => {
    // Two classes, not three: no digit, no special character and nothing
    // outside ASCII. It is long, and it still fails, which is the rule.
    const english = checkPassword("correct horse battery staple");
    expect(english.satisfied).toEqual(["lower"]);
    expect(english.acceptable).toBe(false);

    // The German one passes, on upper, lower and the ß.
    const german = checkPassword("Korrektes Pferd Batterie Klammerß");
    expect(german.acceptable).toBe(true);
  });

  it("counts code points rather than bytes", () => {
    expect(passwordLength("😀".repeat(10))).toBe(10);
    expect(checkPassword("Straße1".padEnd(12, "x")).acceptable).toBe(true);
  });

  it("refuses more than 256 characters", () => {
    const verdict = checkPassword(`Ab1!${"x".repeat(300)}`);
    expect(verdict.tooLong).toBe(true);
    expect(verdict.acceptable).toBe(false);
  });
});
