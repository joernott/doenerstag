// Modals, the confirmation dialog and the base components.

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { button, field, input, select } from "../src/components/forms";
import { closeAllModals, confirmDialog, openModal } from "../src/components/modal";
import { addTile, tile, tileGrid } from "../src/components/tiles";
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

  it("marks an invalid control and announces the reason", () => {
    const control = input({ name: "name" });
    const rendered = field({ label: "Name", control, error: "That name is taken." });

    expect(control.getAttribute("aria-invalid")).toBe("true");
    expect(control.getAttribute("aria-describedby")).toBe(`${control.id}-error`);
    expect(rendered.querySelector(".field-error")?.getAttribute("role")).toBe("alert");
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
      tile({ href: "/orders/1" }, document.createTextNode("Pinar")),
    );

    expect(grid.getAttribute("role")).toBe("list");
    expect(grid.querySelectorAll("[role='listitem']").length).toBe(2);
    expect(grid.querySelector("a")?.getAttribute("href")).toBe("/orders/new");
  });

  it("says an expired order is expired rather than only fading it", () => {
    const expired = tile(
      { href: "/orders/1", faded: true },
      document.createTextNode("Closed"),
    );

    expect(expired.querySelector(".tile-faded")).not.toBeNull();
    // Opacity is not information: the state is in the text as well.
    expect(expired.textContent).toContain("Closed");
  });
});
