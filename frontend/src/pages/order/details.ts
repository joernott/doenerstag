// The order's own facts and the controls for the order as a whole: its title
// line, the card with its times, the restaurant and the two jobs, and the
// editor and the deletion. Part of the order page (20.29).

import { api } from "../../api";
import { accountChoices, listAccounts } from "../../accounts";
import { contactTarget } from "../../contacts";
import { append, el, keyed, rerender, type Child } from "../../dom";
import { definitionList } from "../../components/definitions";
import {
  formatDateTime,
  formatMoney,
  formatRelativeTime,
  fulfilmentLabel,
  localInputValue,
  orderTitle,
} from "../../format";
import {
  button,
  field,
  fieldError,
  iconButton,
  input,
  select,
} from "../../components/forms";
import { openFormDialog } from "../../components/formdialog";
import { confirmDialog } from "../../components/modal";
import { runAction } from "../../components/status";
import type { Contact } from "../../types";
import { summaryLink } from "../orders";
import { section } from "../page";
import type { OrderContext } from "./context";

export interface OrderDetails {
  /** The card with the order's facts and its two jobs. */
  orderCard: () => HTMLElement;
  /** Redraws the controls beside the page title. */
  renderHeading: () => void;
}

/** The order's own facts and controls; the title controls go into `heading`. */
export function orderDetails(
  ctx: OrderContext,
  heading: HTMLElement,
): OrderDetails {
  const {
    app,
    id,
    restaurant,
    moneyFormat,
    status,
    detail,
    active,
    isCreator,
    isParticipant,
    refresh,
  } = ctx;
  const { t } = app;

  function orderCard(): HTMLElement {
    const rows: [string, Child][] = [
      [t.t("restaurant.data"), restaurantLine()],
      [t.t("order.fulfilment.type"), fulfilmentLabel(t, ctx.order.fulfilment)],
      [
        t.t("order.fulfilment.label"),
        formatDateTime(app.language, ctx.order.fulfilment_at),
      ],
      [
        t.t("order.deadline.label"),
        `${formatDateTime(app.language, ctx.order.deadline_at)} (${formatRelativeTime(
          app.language,
          ctx.order.deadline_at,
        )})`,
      ],
    ];

    // Who does what. Only an authenticated caller is told, because both are
    // named people (ADR-0011), and both rows are shown even when empty: an
    // order with nobody doing a job is the case worth seeing, and it is where
    // the "Me!" button goes.
    const own = detail();
    if (own) {
      rows.push([
        t.t("order.money_collector"),
        personRow(
          own.money_collector_name,
          own.money_collector_id,
          "money-collector",
          t.t("order.money_collector"),
        ),
      ]);
      rows.push([
        t.t("order.pickup_person"),
        personRow(
          own.pickup_person_name,
          own.pickup_person_id,
          "pickup-person",
          t.t("order.pickup_person"),
        ),
      ]);
      rows.push([t.t("order.creator"), own.creator_name]);
    }
    if (ctx.order.min_order_value_cents !== null) {
      rows.push([
        t.t("order.minimum"),
        formatMoney(
          app.language,
          ctx.order.min_order_value_cents,
          ctx.order.currency_code,
          moneyFormat,
        ),
      ]);
    }
    if (ctx.order.delivery_fee_cents !== null) {
      rows.push([
        t.t("order.delivery_fee"),
        formatMoney(
          app.language,
          ctx.order.delivery_fee_cents,
          ctx.order.currency_code,
          moneyFormat,
        ),
      ]);
    }

    const list = definitionList(rows);

    // Editing and deleting the order live on the title's line with the summary,
    // not in this card: all three act on the order as a whole rather than on
    // anything inside the card, and they were the only reason it had a row of
    // buttons at all.
    return section(orderTitle(app.language, ctx.order), list, status.element);
  }

  /**
   * The controls on the title's line: summary, edit, delete.
   *
   * Rebuilt on every refresh rather than built once, because whether they apply
   * changes while the page is open. Adding the first item of your own makes you
   * a participant and unlocks the summary; removing your last one locks it
   * again; the deadline passing takes the edit away. Built once, the summary
   * stayed locked for the rest of the visit however many items you added.
   */
  function renderHeading(): void {
    const controls: Child[] = [
      keyed(summaryLink(app, id, isParticipant()), "summary"),
    ];

    // F5.7: the creator edits while the order is active. F6.6 makes everything
    // read-only afterwards, for the administrator too, so there is nothing to
    // show then -- and a control that cannot be used is not shown at all.
    if (isCreator() && active()) {
      controls.push(
        keyed(
          button({
            label: t.t("order.edit"),
            onclick: () => openOrderEditor(),
          }),
          "edit-order",
        ),
      );
    }
    if ((isCreator() || app.session.isAdmin) && detail()) {
      controls.push(
        keyed(
          button({
            label: t.t("order.delete"),
            variant: "danger",
            onclick: () => remove(),
          }),
          "delete-order",
        ),
      );
    }

    rerender(heading, ...controls);
  }

  /** The restaurant, with its contacts as the links they are meant to be. */
  /**
   * One of the two jobs an order has, and a way to take it.
   *
   * The row is shown even when nobody is doing the job, because that is the
   * case worth seeing. The button is offered to every signed-in visitor and not
   * only to the creator: the point of it is that whoever is willing can say so
   * without finding the creator first, which is how this gets decided in the
   * room anyway. It is not offered on a closed order, where nothing can be
   * changed, and never to an anonymous visitor, who has no name to put there.
   * Fetching the food got it in 17.8 and collecting the money in 19.3.
   *
   * A job somebody has carries an × instead (19.5), for the people the server
   * lets clear it: whoever is doing it, so that "Me!" can be taken back without
   * finding the creator, the creator, and the administrator. Open orders only,
   * like everything else that changes the order.
   */
  function personRow(
    name: string,
    holderID: string | null,
    job: "pickup-person" | "money-collector",
    jobName: string,
  ): Child {
    if (name) {
      const me = app.session.user?.id;
      const mayRelease =
        me !== undefined &&
        active() &&
        (app.session.isAdmin || isCreator() || holderID === me);
      if (!mayRelease) {
        return name;
      }
      return el(
        "span",
        { class: "order-person" },
        el("span", { text: name }),
        // The same key as "Me!": pressing one replaces it with the other, and
        // focus follows to the control that undoes what was just done.
        keyed(
          iconButton("close", `${t.t("order.release")}: ${jobName}`, "", () => {
            void release(job);
          }),
          `job:${job}`,
        ),
      );
    }

    const nobody = el("span", { class: "muted", text: t.t("order.nobody") });
    if (!app.session.isAuthenticated || !active()) {
      return nobody;
    }

    return el(
      "span",
      { class: "order-person" },
      nobody,
      keyed(
        button({
          label: t.t("order.volunteer"),
          // Two buttons both called "Me!" cannot be told apart by ear.
          ariaLabel: `${t.t("order.volunteer")}: ${jobName}`,
          onclick: () => volunteer(job),
        }),
        `job:${job}`,
      ),
    );
  }

  /**
   * Takes on one of the two jobs.
   *
   * Its own endpoint rather than a PATCH, because PATCH belongs to the creator
   * and this deliberately does not. Putting "unless the only field is
   * pickup_person_id and its value is your own id and it was empty before"
   * inside the general edit path would be a rule nobody could find later.
   */
  async function volunteer(
    job: "pickup-person" | "money-collector",
  ): Promise<void> {
    await runAction({ status, t }, async () => {
      await api.post(`/orders/${id}/${job}`, {});
      await refresh();
    });
  }

  /** Leaves one of the two jobs vacant again. */
  async function release(
    job: "pickup-person" | "money-collector",
  ): Promise<void> {
    await runAction({ status, t }, async () => {
      await api.delete(`/orders/${id}/${job}`);
      await refresh();
    });
  }

  function restaurantLine(): HTMLElement {
    const line = el("div", { class: "restaurant-line" });
    append(
      line,
      el("a", {
        class: "link",
        href: `/restaurants/${ctx.order.restaurant_id}`,
        text: ctx.order.restaurant_name,
      }),
    );

    for (const contact of restaurant?.contacts ?? []) {
      line.appendChild(contactLink(contact));
    }
    return line;
  }

  /**
   * One contact, as the link it is meant to be.
   *
   * Where it leads comes from contacts.ts, which is where that rule lives for
   * every page that shows one. This function used to decide for itself and had
   * drifted: an address fell through to the text branch because the list here
   * had never gained the case, a telephone number kept the spaces people write
   * it with, and a website typed without a scheme became a relative link.
   *
   * A contact with no label is its own label. Printing "value: value" -- which
   * is what the text branch did -- says the address twice and reads as though
   * something is missing.
   */
  function contactLink(contact: Contact): HTMLElement {
    const label = contact.label.trim();
    const target = contactTarget(contact);

    if (!target) {
      return el("span", {
        class: "contact muted",
        text: label ? `${label}: ${contact.value}` : contact.value,
      });
    }

    return el("a", {
      class: "link contact",
      href: target.href,
      ...(target.external ? { rel: "noreferrer", target: "_blank" } : {}),
      text: label || contact.value,
    });
  }

  // --- the order's own fields ---------------------------------------------

  async function openOrderEditor(): Promise<void> {
    const own = detail();
    if (!own) {
      return;
    }

    // The accounts to choose between, fetched when the dialog is opened rather
    // than with the page: only the creator ever opens it, and an anonymous
    // visitor would be refused the list.
    const accounts = await listAccounts().catch(() => []);

    const fulfilment = select(
      [
        { value: "pickup", label: t.t("order.fulfilment.pickup") },
        { value: "delivery", label: t.t("order.fulfilment.delivery") },
      ],
      { value: ctx.order.fulfilment },
    );
    const fulfilmentAt = input({
      type: "datetime-local",
      value: localInputValue(ctx.order.fulfilment_at),
    });
    const deadlineAt = input({
      type: "datetime-local",
      value: localInputValue(ctx.order.deadline_at),
    });
    const deadlineProblem = fieldError(deadlineAt);
    const choices = accountChoices(accounts, t.t("order.nobody"));
    const moneyCollector = select(choices, {
      value: own.money_collector_id ?? "",
    });
    const pickupPerson = select(choices, { value: own.pickup_person_id ?? "" });

    // F5.8: the restaurant is locked once the order has items, because the
    // prices and the currency were copied from it. Disabled with the reason
    // rather than hidden, or the absence would look like a bug.
    const restaurantNote =
      ctx.order.item_count > 0
        ? el("p", { class: "field-hint", text: t.t("order.restaurant_locked") })
        : null;

    openFormDialog({
      t,
      title: t.t("order.edit"),
      className: "modal-narrow",
      fields: [
        field({ label: t.t("order.fulfilment.type"), control: fulfilment }),
        field({ label: t.t("order.fulfilment.label"), control: fulfilmentAt }),
        field({
          label: t.t("order.deadline.label"),
          control: deadlineAt,
          error: deadlineProblem,
        }),
        restaurantNote,
        field({
          label: t.t("order.money_collector"),
          control: moneyCollector,
          optionalLabel: t.t("auth.optional"),
        }),
        field({
          label: t.t("order.pickup_person"),
          control: pickupPerson,
          optionalLabel: t.t("auth.optional"),
        }),
      ],
      onSubmit: async () => {
        const fulfils = new Date(fulfilmentAt.value);
        const closes = new Date(deadlineAt.value);
        if (
          Number.isNaN(fulfils.getTime()) ||
          closes.getTime() >= fulfils.getTime()
        ) {
          deadlineProblem.set(t.t("order.deadline_before_fulfilment"));
          deadlineAt.focus();
          return false;
        }
        deadlineProblem.clear();
        await api.patch(`/orders/${id}`, {
          fulfilment: fulfilment.value,
          fulfilment_at: fulfils.toISOString(),
          deadline_at: closes.toISOString(),
          money_collector_id: moneyCollector.value,
          pickup_person_id: pickupPerson.value,
        });
        return true;
      },
      after: refresh,
    });
  }

  async function remove(): Promise<void> {
    const own = detail();
    // How many other people lose their items, named rather than described
    // (F5.9).
    const others = new Set(
      (own?.items ?? [])
        .filter((item) => item.user_id !== app.session.user?.id)
        .map((item) => item.user_id),
    );

    const agreed = await confirmDialog({
      t,
      message: t.t("confirm.delete_order"),
      ...(others.size > 0
        ? {
            detail: t.t("confirm.delete_order.participants", {
              count: others.size,
            }),
          }
        : {}),
      confirmLabel: t.t("action.delete"),
    });
    if (!agreed) {
      return;
    }

    await runAction({ status, t }, async () => {
      await api.delete(`/orders/${id}`);
      app.navigate("/");
    });
  }

  return { orderCard, renderHeading };
}
