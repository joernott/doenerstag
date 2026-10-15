// Modals, the confirmation dialog and the base components.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { button, field, fieldError, input, select } from "../src/components/forms";
import { impactList } from "../src/components/deleteaccount";
import { openFormDialog } from "../src/components/formdialog";
import { closeAllModals, confirmDialog, openModal } from "../src/components/modal";
import { addTile, tile, tileGrid } from "../src/components/tiles";
import { keyed, rerender } from "../src/dom";
import { Translator } from "../src/i18n";

const t = new Translator("en");

beforeEach(() => {
  document.body.replaceChildren();
  const root = document.createElement("div");
  root.id = "app-root";
  document.body.appendChild(root);
});

afterEach(() => {
  closeAllModals();
});

function dialog(): HTMLElement {
  const found = document.querySelector<HTMLElement>(".modal");
  if (!found) {
    throw new Error("no modal is open");
  }
  return found;
}

describe("a modal", () => {
  it("is a labelled dialog", () => {
    openModal({ title: "Delete order", body: "Gone for good.", closeLabel: "Close" });

    expect(dialog().getAttribute("role")).toBe("dialog");
    expect(dialog().getAttribute("aria-modal")).toBe("true");
    const labelledBy = dialog().getAttribute("aria-labelledby") ?? "";
    expect(document.getElementById(labelledBy)?.textContent).toBe("Delete order");
  });

  it("makes the page behind it inert and lets it go again", () => {
    const modal = openModal({ title: "Title", body: "Body", closeLabel: "Close" });
    expect(document.getElementById("app-root")?.hasAttribute("inert")).toBe(true);

    modal.close();
    expect(document.getElementById("app-root")?.hasAttribute("inert")).toBe(false);
  });

  it("closes on Escape and returns focus to whatever opened it", () => {
    const opener = document.createElement("button");
    document.getElementById("app-root")?.appendChild(opener);
    opener.focus();

    openModal({ title: "Title", body: "Body", closeLabel: "Close" });
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));

    expect(document.querySelector(".modal")).toBeNull();
    expect(document.activeElement).toBe(opener);
  });

  it("closing twice is harmless", () => {
    const modal = openModal({ title: "Title", body: "Body", closeLabel: "Close" });
    modal.close();
    expect(() => modal.close()).not.toThrow();
  });

  it("keeps Tab inside itself", () => {
    const modal = openModal({
      title: "Title",
      body: "Body",
      closeLabel: "Close",
      actions: [button({ label: "Cancel" }), button({ label: "Confirm" })],
    });

    const stops = [...modal.element.querySelectorAll<HTMLElement>("button")];
    const last = stops[stops.length - 1];
    last?.focus();

    // jsdom does not move focus for Tab by itself; what is asserted is that the
    // trap intervened, which is the part this application implements.
    const tab = new KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true });
    document.dispatchEvent(tab);

    expect(tab.defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(stops[0]);
  });
});

describe("the confirmation dialog", () => {
  it("resolves true only when the person confirms", async () => {
    const answer = confirmDialog({ t, message: "This deletes the order." });
    const confirm = [...document.querySelectorAll<HTMLButtonElement>(".modal-actions button")].at(-1);
    confirm?.click();

    await expect(answer).resolves.toBe(true);
  });

  it("treats every other way out as a no", async () => {
    const answer = confirmDialog({ t, message: "This deletes the order." });
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));

    await expect(answer).resolves.toBe(false);
  });

  it("focuses cancel, so a stray Enter destroys nothing", () => {
    void confirmDialog({ t, message: "This deletes the order." });
    const cancel = document.querySelector<HTMLButtonElement>(".modal-actions button");

    expect(document.activeElement).toBe(cancel);
    expect(cancel?.textContent).toBe(t.t("action.cancel"));
  });

  it("names the collateral effect when there is one", () => {
    void confirmDialog({
      t,
      message: t.t("confirm.delete_order"),
      detail: t.t("confirm.delete_order.participants", { count: 3 }),
    });

    expect(dialog().textContent).toContain("3 other people have items");
  });
});

describe("form controls", () => {
  it("binds a label to its control and describes its hint", () => {
    const control = input({ name: "password", type: "password" });
    const rendered = field({ label: "Password", control, hint: "Ten characters." });

    const label = rendered.querySelector("label");
    expect(label?.getAttribute("for")).toBe(control.id);
    expect(control.getAttribute("aria-describedby")).toBe(`${control.id}-hint`);
  });

  // 20.27: an error appears and goes with the value, and while it is there the
  // control says so and is described by it, alongside any hint it already had.
  it("marks an invalid control and announces the reason, and takes both back", () => {
    const control = input({ name: "name" });
    const error = fieldError(control);
    const rendered = field({ label: "Name", control, hint: "As on the menu.", error });
    const hint = `${control.id}-hint`;
    const errorId = `${control.id}-error`;

    expect(control.getAttribute("aria-invalid")).toBeNull();
    expect(control.getAttribute("aria-describedby")).toBe(hint);

    error.set("That name is taken.");
    expect(control.getAttribute("aria-invalid")).toBe("true");
    expect(control.getAttribute("aria-describedby")).toBe(`${hint} ${errorId}`);
    const message = rendered.querySelector<HTMLElement>(".field-error");
    expect(message?.getAttribute("role")).toBe("alert");
    expect(message?.textContent).toBe("That name is taken.");

    error.clear();
    expect(control.getAttribute("aria-invalid")).toBeNull();
    expect(control.getAttribute("aria-describedby")).toBe(hint);
    expect(message?.hidden).toBe(true);
  });

  it("explains a disabled control rather than leaving it a mystery", () => {
    const rendered = button({
      label: "Change restaurant",
      disabled: true,
      title: "The order already has items.",
    });

    expect(rendered.disabled).toBe(true);
    expect(rendered.title).toBe("The order already has items.");
  });

  it("selects the current value", () => {
    const rendered = select(
      [
        { value: "EUR", label: "Euro" },
        { value: "CHF", label: "Swiss franc" },
      ],
      { value: "CHF" },
    );
    expect(rendered.value).toBe("CHF");
  });
});

describe("tiles", () => {
  it("is a list of tiles, with the plus first", () => {
    const grid = tileGrid(
      addTile("/orders/new", "New order"),
      tile({ href: "/orders/1", title: "Pinar" }),
    );

    expect(grid.getAttribute("role")).toBe("list");
    expect(grid.querySelectorAll("[role='listitem']").length).toBe(2);
    expect(grid.querySelector("a")?.getAttribute("href")).toBe("/orders/new");
  });

  it("says an expired order is expired rather than only fading it", () => {
    const expired = tile(
      { href: "/orders/1", title: "Pinar", faded: true },
      document.createTextNode("Closed"),
    );

    expect(expired.querySelector(".tile-faded")).not.toBeNull();
    // Opacity is not information: the state is in the text as well.
    expect(expired.textContent).toContain("Closed");
  });
});

// 20.12: rebuilding a region keeps the keyboard where it was.
describe("rerender", () => {
  it("puts focus on the replacement of the control that had it", () => {
    const region = document.createElement("div");
    document.body.appendChild(region);
    const build = (): HTMLElement[] => [
      keyed(document.createElement("button"), "first"),
      keyed(document.createElement("button"), "second"),
    ];

    rerender(region, ...build());
    const before = region.querySelectorAll("button")[1];
    before?.focus();

    rerender(region, ...build());
    const after = region.querySelectorAll("button")[1];

    expect(after).not.toBe(before);
    expect(document.activeElement).toBe(after);
    region.remove();
  });

  it("keeps the caret where it was in a text field", () => {
    const region = document.createElement("div");
    document.body.appendChild(region);
    const field = (): HTMLInputElement => {
      const input = keyed(document.createElement("input"), "name");
      input.value = "Döner Palast";
      return input;
    };

    rerender(region, field());
    const before = region.querySelector("input");
    before?.focus();
    before?.setSelectionRange(3, 3);

    rerender(region, field());
    const after = region.querySelector("input");
    expect(document.activeElement).toBe(after);
    expect(after?.selectionStart).toBe(3);
    region.remove();
  });

  it("leaves focus alone when it was outside the region", () => {
    const region = document.createElement("div");
    const outside = document.createElement("button");
    document.body.append(region, outside);
    outside.focus();

    rerender(region, keyed(document.createElement("button"), "inside"));

    expect(document.activeElement).toBe(outside);
    region.remove();
    outside.remove();
  });
});

// 20.25: one form dialog for every editor. Its footer Save submits the form it
// sits outside of -- the wiring 18.1 found missing twice -- and so does Enter.
describe("a form dialog", () => {
  function footerSubmit(): HTMLButtonElement {
    const found = dialog().querySelector<HTMLButtonElement>(".modal-actions button[type='submit']");
    if (!found) {
      throw new Error("no submit button in the footer");
    }
    return found;
  }

  it("submits from the footer button and from Enter in a field", async () => {
    const name = input({ name: "name" });
    const onSubmit = vi.fn(() => Promise.resolve(false));
    openFormDialog({ t, title: "Name", fields: [field({ label: "Name", control: name })], onSubmit });

    footerSubmit().click();
    await Promise.resolve();
    expect(onSubmit).toHaveBeenCalledTimes(1);

    name.closest("form")?.requestSubmit();
    await Promise.resolve();
    expect(onSubmit).toHaveBeenCalledTimes(2);
  });

  it("stays open and says why when the work fails", async () => {
    openFormDialog({
      t,
      title: "Name",
      fields: [],
      onSubmit: () => Promise.reject(new Error("the database is unavailable")),
    });

    footerSubmit().click();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(document.querySelector(".modal")).not.toBeNull();
    expect(dialog().querySelector(".status-error")).not.toBeNull();
  });

  it("closes, gives focus back, and only then runs what comes after", async () => {
    const opener = button({ label: "Open" });
    document.body.appendChild(opener);
    opener.focus();

    let focusedWhenAfterRan: Element | null = null;
    openFormDialog({
      t,
      title: "Name",
      fields: [],
      onSubmit: () => Promise.resolve(),
      after: () => {
        focusedWhenAfterRan = document.activeElement;
      },
    });

    footerSubmit().click();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(document.querySelector(".modal")).toBeNull();
    expect(focusedWhenAfterRan).toBe(opener);
  });
});

// 20.7.1: deleting an account warns about the jobs it holds as well. The server
// counted them, but the dialog listed only items and orders opened, so somebody
// collecting the money for tomorrow's order was told nothing would change.
describe("the deletion warning", () => {
  it("names the open orders whose money or food the account looks after", () => {
    const list = impactList(t, {
      active_orders: 2,
      active_items: 0,
      expired_items: 0,
      created_active_orders: 0,
      active_jobs: 2,
    });
    expect(list.textContent).toContain(t.t("account.delete.impact.jobs", { count: 2 }));
    expect(list.textContent).not.toContain(t.t("account.delete.impact.none"));
  });
});
