// The shapes the API answers with, as the pages and components use them.
//
// They lived in whichever page first needed them -- RestaurantDetail was
// declared five times, two of them without its opening hours, and the order
// types were exported from the order overview page -- so a component that
// needed one had to import a page (20.28).

import type { Classification, Tag } from "./reference";

/** A restaurant as the API sends it. */
export interface Restaurant {
  id: string;
  name: string;
  logo_image_id: string | null;
  currency_code: string;
  min_order_value_cents: number | null;
  delivery_fee_cents: number | null;
  notes: string;
}

export interface Contact {
  id: string;
  contact_type_id: string;
  contact_type_code: string;
  render_as: string;
  value: string;
  label: string;
  sort_order: number;
}

export interface OpeningPeriod {
  id: string;
  day_of_week: number;
  start: string;
  end: string;
  crosses_midnight: boolean;
}

export interface RestaurantDetail extends Restaurant {
  contacts: Contact[];
  opening_hours: OpeningPeriod[];
}

/**
 * A named rule about when food can be had.
 *
 * Its own parts are ANDed: weekdays plus a time means those days and only
 * during those hours. A part left null has no opinion. Several filters on one
 * element are alternatives; a category's and an item's must both hold. The
 * server decides all of that -- this type is what the page shows and edits.
 */
export interface AvailabilityFilter {
  id: string;
  restaurant_id: string;
  name: string;
  /** "YYYY-MM-DD", or null for any date. */
  on_date: string | null;
  /** ISO days, 1 = Monday. Empty means every day. */
  weekdays: number[];
  /** "HH:MM", or null for any time. Both or neither. */
  start_time: string | null;
  end_time: string | null;
  sort_order: number;
}

export interface Category {
  id: string;
  name: string;
  sort_order: number;
  /** The filters attached to this category. Empty means always available. */
  availability?: AvailabilityFilter[];
}

export interface Modification {
  id: string;
  name: string;
  price_delta_cents: number;
  sort_order: number;
}

export interface MenuItem {
  id: string;
  category_id: string | null;
  external_id: string;
  name: string;
  description: string;
  image_id: string | null;
  price_cents: number;
  available: boolean;
  tags: Tag[];
  allergens: Classification[];
  additives: Classification[];
  modifications?: Modification[];

  /** The filters attached to this item. Empty means always available. */
  availability?: AvailabilityFilter[];

  /**
   * Whether it can be had at the moment the menu was asked about: this item's
   * filters and its category's taken together, decided by the server. Null when
   * no moment was named, which is how the restaurant page asks -- it shows the
   * menu, because it is the menu.
   */
  available_at?: boolean | null;
}

/** The order header, which every caller receives. */
export interface OrderHeader {
  id: string;
  title: string;
  restaurant_id: string;
  restaurant_name: string;
  restaurant_logo_image_id: string | null;
  fulfilment: string;
  fulfilment_at: string;
  deadline_at: string;
  status: string;
  currency_code: string;
  min_order_value_cents: number | null;
  delivery_fee_cents: number | null;
  item_count: number;
}

/**
 * The list entry a logged-in caller receives: the header plus the people.
 *
 * Who opened the order, who collects the money and who fetches the food are all
 * here rather than on OrderHeader, because OrderHeader is what an anonymous
 * caller is served and it names nobody (ADR-0011).
 */
export interface OrderListEntry extends OrderHeader {
  creator_id?: string;
  creator_name?: string;
  money_collector_id?: string | null;
  money_collector_name?: string;
  pickup_person_id?: string | null;
  pickup_person_name?: string;
}

/** One order item, which only an authenticated caller ever sees. */
export interface OrderItem {
  id: string;
  user_id: string;
  user_name: string;
  menu_item_id: string;
  quantity: number;
  item_name: string;
  unit_price_cents: number;
  note: string;
  modifications: { id: string; modification_id: string | null; name: string; price_delta_cents: number }[];
  line_total_cents: number;
  /** Ticked as settled. Not a payment: the application takes none. */
  paid: boolean;
}

/** The authenticated order shape: the header, the creator and the items. */
export interface OrderDetail extends OrderHeader {
  creator_id: string;
  creator_name: string;
  money_collector_id: string | null;
  money_collector_name: string;
  pickup_person_id: string | null;
  pickup_person_name: string;
  items: OrderItem[];
  item_total_cents: number;
  /** The item total split by the paid tick; the two always add up to it. */
  unpaid_total_cents: number;
  paid_total_cents: number;
  grand_total_cents: number;
  below_minimum: boolean;
}

export interface DeletionImpact {
  active_orders: number;
  active_items: number;
  expired_items: number;
  created_active_orders: number;
}

