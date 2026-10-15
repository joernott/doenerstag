// The restaurant page: the restaurant itself, its contacts, its opening hours
// and its menu, stacked as docs/06_ui_ux.md describes.
//
// Everything here is editable by any logged-in user. Only deletion is the
// administrator's, and only the administrator is offered it.
//
// This module loads the restaurant and lays the page out; the sections are in
// the modules beside it: create.ts, data.ts, contacts.ts and hours.ts.

import type { App } from "../../app";
import { api, getList } from "../../api";
import { el } from "../../dom";
import { button } from "../../components/forms";
import { saveAllButton, type EditableSection } from "../../components/editable";
import { referenceData, type ReferenceData } from "../../reference";
import { availabilitySection } from "../availability";
import { tabs } from "../../components/tabs";
import { menuSection } from "../menu";
import { failedPage, pageWithActions, setPageTitle } from "../page";
import type { RestaurantDetail } from "../../types";
import { contactsSection } from "./contacts";
import { createPage } from "./create";
import { dataSection, type DataSection } from "./data";
import { hoursSection } from "./hours";

/** The page for one restaurant, or the form that creates one. */
export async function restaurantPage(app: App, id: string): Promise<HTMLElement> {
  const { t } = app;

  let reference: ReferenceData;
  try {
    reference = await referenceData();
  } catch (error) {
    return failedPage(t.t("nav.restaurants"), t, error);
  }

  if (id === "new") {
    return createPage(app, reference);
  }

  let restaurant: RestaurantDetail;
  try {
    restaurant = await api.get<RestaurantDetail>(`/restaurants/${id}`);
  } catch (error) {
    return failedPage(t.t("nav.restaurants"), t, error);
  }

  // Whether anything still points at this restaurant. Deleting one that an
  // order references is refused with 4003, and finding that out by pressing the
  // button and reading an error is a worse answer than a button that says so
  // before it is pressed. The list is small (ADR-0006) and this is one request.
  const blocking = await getList<{ restaurant_id: string }>("/orders", "orders")
    .then((orders) => orders.some((order) => order.restaurant_id === restaurant.id))
    .catch(() => false);

  const data = dataSection(app, reference, restaurant);
  const hours = hoursSection(app, restaurant);
  const header = restaurantControls(app, data, hours, blocking);

  const article = pageWithActions(
    restaurant.name,
    header,
    // The menu first: it is what somebody opening a restaurant almost always
    // came for, and it used to be the section they had to scroll past three
    // others to reach.
    tabs(
      [
        {
          id: "menu",
          label: t.t("restaurant.menu"),
          panel: menuSection(app, reference, restaurant),
        },
        { id: "data", label: t.t("restaurant.data"), panel: data.element },
        {
          id: "contacts",
          label: t.t("restaurant.contacts"),
          panel: contactsSection(app, reference, restaurant),
        },
        {
          id: "hours",
          label: t.t("restaurant.opening_hours"),
          panel: hours.element,
        },
        {
          id: "availability",
          label: t.t("availability.title"),
          panel: availabilitySection(app, restaurant.id),
        },
      ],
      { label: t.t("restaurant.sections"), initial: 0 },
    ),
  );

  data.onRename((next) => setPageTitle(article, next));
  return article;
}

/**
 * Save and Delete, on the title's line.
 *
 * Save belongs to the restaurant, not to the tab that happens to hold its
 * fields, so it sits with the name and stays reachable from any tab. It is
 * faded until something has actually changed: a Save that is always available
 * says nothing about whether there is anything to save.
 *
 * Delete is faded rather than hidden when it cannot be used, which is a
 * departure from the rule in docs/06_ui_ux.md about not showing controls that
 * would be refused. The difference is that this one explains itself: its title
 * says whether the obstacle is the permission or an order still pointing at the
 * restaurant, and that is more useful than the button silently not being there.
 */
function restaurantControls(
  app: App,
  data: DataSection,
  hours: EditableSection,
  blocking: boolean,
): HTMLElement {
  const { t } = app;

  // One Save for the restaurant, covering its own fields and its opening hours:
  // two buttons called Save on one page, each covering a different part of it,
  // is a question nobody should have to answer.
  const save = saveAllButton(t, [data, hours]);

  const why = !app.session.isAdmin
    ? t.t("restaurant.delete.admin_only")
    : blocking
      ? t.t("restaurant.delete.in_use")
      : "";

  const remove = button({
    label: t.t("restaurant.delete"),
    variant: "danger",
    ...(why ? { title: why } : {}),
    onclick: () => data.remove(),
  });
  remove.disabled = why !== "";

  return el("div", { class: "page-heading-actions" }, save, remove);
}
