// A page made of sections with something to save, and the one Save on its
// heading that writes whichever of them changed.
//
// The restaurant page (its data and its opening hours) and the account page
// (the profile and the password) each wrote this out: the snapshot, the
// listeners, the comparison, the heading button tracking two flags. The copies
// had already drifted -- one section swallowed its own failure and left Save
// disabled for good (20.10) -- so it is written once (20.26).

import type { Translator } from "../i18n";
import { button } from "./forms";

/** A section of a page that has something to save. */
export interface EditableSection {
  element: HTMLElement;
  /**
   * Writes the section. Reports its own failure where the section is, and
   * rejects, so the heading's Save knows to let somebody try again.
   */
  save(): Promise<void>;
  onDirtyChange(listen: (dirty: boolean) => void): void;
}

/** Tells whether a section differs from what was last saved. */
export interface DirtyTracker {
  /** Call on every change to the section's controls. */
  touched: () => void;
  /** Call once a save has gone through: what is there now is the baseline. */
  saved: () => void;
  onDirtyChange: (listen: (dirty: boolean) => void) => void;
}

/**
 * Dirty means "differs from what was loaded or last saved", not "somebody
 * pressed a key": typing a change and then undoing it leaves nothing to save.
 * The snapshot is any string that changes exactly when the section's content
 * does.
 */
export function dirtyTracker(snapshot: () => string): DirtyTracker {
  let baseline = snapshot();
  const listeners: ((dirty: boolean) => void)[] = [];
  const report = (): void => {
    const dirty = snapshot() !== baseline;
    for (const listen of listeners) {
      listen(dirty);
    }
  };
  return {
    touched: report,
    saved: () => {
      baseline = snapshot();
      report();
    },
    onDirtyChange: (listen) => {
      listeners.push(listen);
    },
  };
}

/**
 * The heading's Save: enabled while any section has changes, and writing only
 * those that have, in order, so pressing it never sends a request that changes
 * nothing. A section that fails has said so in its own status line; all that
 * is left here is to let somebody try again.
 */
export function saveAllButton(t: Translator, sections: EditableSection[]): HTMLButtonElement {
  const dirty = sections.map(() => false);
  const save = button({
    label: t.t("action.save"),
    variant: "primary",
    onclick: async () => {
      save.disabled = true;
      try {
        for (const [index, section] of sections.entries()) {
          if (dirty[index]) {
            await section.save();
          }
        }
      } catch {
        save.disabled = false;
      }
    },
  });
  save.disabled = true;

  sections.forEach((section, index) => {
    section.onDirtyChange((changed) => {
      dirty[index] = changed;
      save.disabled = !dirty.some(Boolean);
    });
  });
  return save;
}
