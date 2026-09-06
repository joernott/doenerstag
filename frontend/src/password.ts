// The password complexity rules, as the person typing sees them.
//
// This mirrors internal/auth/complexity.go deliberately and exactly: the same
// five classes, the same special-character set, the same NFC normalisation, the
// same three-of-five threshold. Two implementations of one rule is a thing to
// be uneasy about, but the alternative -- asking the server on every keystroke
// -- would send the password over the wire before it is finished, repeatedly,
// and would still have to work when the answer arrives after the next
// character. The server enforces the rule; this only says what it will decide.
//
// docs/05_auth_and_permissions.md is the specification both sides implement.

/** The class 4 set, exactly as the specification lists it. */
export const SPECIAL_CHARACTERS = `<>|-_.:,;#'!"§$%&/()[]{}?@`;

/** Code points, not bytes: ten characters of Cyrillic are ten characters. */
export const MIN_LENGTH = 10;
export const MAX_LENGTH = 256;

/** How many of the five classes a password must satisfy. */
export const REQUIRED_CLASSES = 3;

/** The five classes, in the order the specification gives them. */
export const CLASSES = ["upper", "lower", "digit", "special", "other"] as const;

export type PasswordClass = (typeof CLASSES)[number];

/**
 * Which classes a password satisfies.
 *
 * Normalises first, so a decomposed "ä" counts as one language-specific
 * character rather than as a bare "a" followed by a combining mark.
 */
export function satisfiedClasses(password: string): PasswordClass[] {
  const found = new Set<PasswordClass>();

  for (const character of password.normalize("NFC")) {
    if (character >= "A" && character <= "Z") {
      found.add("upper");
    } else if (character >= "a" && character <= "z") {
      found.add("lower");
    } else if (character >= "0" && character <= "9") {
      found.add("digit");
    } else if (SPECIAL_CHARACTERS.includes(character)) {
      found.add("special");
    } else if (isExtended(character)) {
      found.add("other");
    }
  }

  return CLASSES.filter((name) => found.has(name));
}

/**
 * Class 5, defined by exclusion rather than by a list.
 *
 * Enumerating accented characters would leave out somebody's alphabet. The rule
 * is "a printable character none of the first four classes covers", which
 * admits every alphabet, every currency symbol and every punctuation mark that
 * is not in SPECIAL_CHARACTERS. A plain space earns nothing on its own, so
 * "hello world" does not pass a class it has not earned, but stays legal inside
 * a passphrase.
 */
function isExtended(character: string): boolean {
  if (character === " ") {
    return false;
  }
  if (!/[\p{L}\p{M}\p{N}\p{P}\p{S}]/u.test(character)) {
    return false;
  }
  return !SPECIAL_CHARACTERS.includes(character);
}

/** The length in code points, which is what the rule counts. */
export function passwordLength(password: string): number {
  return [...password.normalize("NFC")].length;
}

export interface PasswordVerdict {
  /** The classes satisfied so far. */
  satisfied: PasswordClass[];
  /** Whether the length is within bounds. */
  longEnough: boolean;
  tooLong: boolean;
  /** Whether the server would accept it. */
  acceptable: boolean;
}

/** What the indicator shows, and what the server will decide. */
export function checkPassword(password: string): PasswordVerdict {
  const satisfied = satisfiedClasses(password);
  const length = passwordLength(password);
  const longEnough = length >= MIN_LENGTH;
  const tooLong = length > MAX_LENGTH;

  return {
    satisfied,
    longEnough,
    tooLong,
    acceptable: longEnough && !tooLong && satisfied.length >= REQUIRED_CLASSES,
  };
}
