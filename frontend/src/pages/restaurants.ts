// The restaurant overview: a grid of tiles, the first of which creates one.

import type { App } from "../app";
import { api, errorMessage, getList } from "../api";
import { el } from "../dom";
import { thumbnailURL } from "../components/images";
import { addTile, tile, tileGrid } from "../components/tiles";
import { isOpen } from "../openinghours";
import { page } from "./page";
import type { Contact, OpeningPeriod, Restaurant } from "./restaurant";

interface RestaurantDetail extends Restaurant {
  contacts: Contact[];
  opening_hours: OpeningPeriod[];
}

/** What one tile shows, gathered from the three endpoints that hold it. */
interface TileData {
  restaurant: Restaurant;
  contact: Contact | null;
  open: boolean;
  items: number;
}

export async function restaurantsPage(app: App): Promise<HTMLElement> {
  const { t } = app;

  let restaurants: Restaurant[];
  try {
    restaurants = await getList<Restaurant>("/restaurants", "restaurants");
  } catch (error) {
    return page(t.t("nav.restaurants"), el("p", { class: "field-error", text: errorMessage(t, error) }));
  }

  // Alphabetical, and nothing else: docs/06_ui_ux.md says search and sorting do
  // not earn their place with the handful of restaurants expected.
  restaurants.sort((left, right) => left.name.localeCompare(right.name, app.language));

  // The list endpoint carries the restaurant, not its contacts, its opening
  // hours or its menu -- and the tile shows all three (docs/06_ui_ux.md). They
  // are fetched per restaurant and in parallel, which is two extra requests
  // each. That is affordable precisely because the scale is the one ADR-0006
  // assumes: three to ten restaurants on a local network. It would not be at a
  // thousand, and the answer then would be to widen the list endpoint rather
  // than to make the page cleverer.
  const tiles = await Promise.all(restaurants.map((restaurant) => gather(restaurant)));

  const create = app.session.isAuthenticated ? "/restaurants/new" : "/account";

  return page(
    t.t("nav.restaurants"),
    tileGrid(addTile(create, t.t("restaurant.new")), ...tiles.map((data) => render(app, data))),
  );
}

async function gather(restaurant: Restaurant): Promise<TileData> {
  const [detail, items] = await Promise.all([
    api.get<RestaurantDetail>(`/restaurants/${restaurant.id}`).catch(() => null),
    getList<unknown>(`/restaurants/${restaurant.id}/menu-items`, "menu_items").catch(() => []),
  ]);

  return {
    restaurant,
    contact: detail?.contacts[0] ?? null,
    open: isOpen(detail?.opening_hours ?? []),
    items: items.length,
  };
}

function render(app: App, data: TileData): HTMLElement {
  const { t } = app;

  return tile(
    { href: `/restaurants/${data.restaurant.id}` },
    logo(data.restaurant, t.t("image.current")),
    el("strong", { class: "tile-title", text: data.restaurant.name }),
    el("span", { class: "muted", text: data.contact?.value ?? t.t("restaurant.no_contact") }),
    el("span", {
      class: "muted",
      text: t.t("restaurant.items", { count: data.items }),
    }),
    // In words, not by colour: "open now" and "closed" are the state, and the
    // styling only follows them.
    el("span", {
      class: data.open ? "badge badge-open" : "muted",
      text: data.open ? t.t("restaurant.open_now") : t.t("restaurant.closed"),
    }),
  );
}

/** The logo, or a neutral placeholder. */
function logo(restaurant: Restaurant, alt: string): HTMLElement {
  if (!restaurant.logo_image_id) {
    return el("div", { class: "tile-logo tile-logo-empty", "aria-hidden": "true" });
  }
  return el("img", {
    class: "tile-logo",
    src: thumbnailURL(restaurant.logo_image_id),
    alt,
    loading: "lazy",
  });
}
