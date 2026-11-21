// The vote page (F12, docs/06 "Vote page").
//
// The restaurants on the left, one radio group, so a ballot is one click or
// one arrow key; the pie and the total on the right. The restaurant ahead is
// marked by a border, a tint and a word, never by colour alone, and a screen
// reader hears when the lead changes -- not on every ballot.
//
// The live stream carries the vote without the viewer's own ballot, because a
// stream knows whether its reader is signed in and not who they are (docs/04).
// The page keeps its own choice from the PUT or DELETE it made, and from a
// fresh GET on reconnect.

import type { App } from "../app";
import { api, errorMessage, getList } from "../api";
import { currentPath, loginHref } from "../returnto";
import { el, replace, rerender, type Child } from "../dom";
import {
  EVENT_VOTE_DECIDED,
  EVENT_VOTE_DELETED,
  EVENT_VOTE_UPDATED,
  subscribeToVote,
  type EventSourceFactory,
} from "../events";
import { formatDateTime, formatRelativeTime, localInputValue } from "../format";
import { logoMark } from "../logo";
import { button, checkbox, field, fieldError, iconButton, input, select } from "../components/forms";
import { openFormDialog } from "../components/formdialog";
import { thumbnailURL } from "../components/images";
import { confirmDialog } from "../components/modal";
import { pieChart } from "../components/pie";
import { runAction, statusLine } from "../components/status";
import { failedPage, pageWithActions, section } from "./page";
import type { Restaurant, Vote, VoteChoice } from "../types";

export interface VotePageOptions {
  /** Registers the stream to be closed when the page is replaced. */
  cleanup?: { onCleanup: (undo: () => void) => void };
  /** How an EventSource is made; a test supplies its own. */
  factory?: EventSourceFactory;
}

/** The title a vote is known by, in the viewer's time zone. */
export function voteTitle(app: App, vote: Pick<Vote, "fulfilment_at">): string {
  return app.t.t("vote.title", { when: formatDateTime(app.language, vote.fulfilment_at) });
}

/** Whether ballots are still accepted, on the viewer's clock (F12.8). */
export function voteOpen(vote: Vote, now: Date = new Date()): boolean {
  return vote.status === "open" && new Date(vote.decision_at).getTime() > now.getTime();
}

export async function votePage(app: App, id: string, options: VotePageOptions = {}): Promise<HTMLElement> {
  const { t } = app;

  let vote: Vote;
  try {
    vote = await api.get<Vote>(`/votes/${id}`);
  } catch (error) {
    return failedPage(t.t("vote.badge"), t, error);
  }

  // The viewer's own ballot, which the stream does not carry.
  let mine: string | null = vote.my_choice_id ?? null;
  let leader: string | null = vote.leader_choice_id;

  const status = statusLine();
  const heading = el("div", { class: "page-heading-actions" });
  const banner = el("div");
  const meta = el("div", { class: "vote-meta" });
  const left = el("div", { class: "order-column" });
  const right = el("div", { class: "order-column vote-chart" });

  const me = (): string | undefined => app.session.user?.id;
  const isCreator = (): boolean => me() !== undefined && vote.creator_id === me();

  function render(): void {
    renderHeading();
    renderMeta();
    renderBanner();
    renderList();
    renderChart();
  }

  function renderHeading(): void {
    const controls: HTMLElement[] = [];
    if (isCreator() && voteOpen(vote)) {
      controls.push(button({ label: t.t("vote.edit"), onclick: () => editVote() }));
    }
    if ((isCreator() && voteOpen(vote)) || app.session.isAdmin) {
      controls.push(button({ label: t.t("vote.delete"), variant: "danger", onclick: () => void deleteVote() }));
    }
    heading.replaceChildren(...controls);
  }

  function renderMeta(): void {
    const parts: Child[] = [
      el("span", {
        class: "badge",
        text: vote.public_ballots ? t.t("vote.ballots.public") : t.t("vote.ballots.secret"),
      }),
      el("span", {
        class: "muted",
        text: `${t.t("vote.decision.label")}: ${formatDateTime(app.language, vote.decision_at)}`,
      }),
      el("span", {
        class: "muted",
        text: `${t.t("order.deadline.label")}: ${formatDateTime(app.language, vote.deadline_at)}`,
      }),
      el("span", {
        class: "muted",
        text: `${t.t("order.fulfilment.label")}: ${formatDateTime(app.language, vote.fulfilment_at)}`,
      }),
    ];
    if (voteOpen(vote)) {
      parts.push(
        el("span", {
          class: "tile-hint",
          text: t.t("vote.decided_in", { when: formatRelativeTime(app.language, vote.decision_at) }),
        }),
      );
    }
    // F12.6: an anonymous visitor is told nobody's name, the creator's included.
    if (vote.creator_name) {
      parts.push(el("span", { class: "muted", text: t.t("vote.created_by", { name: vote.creator_name }) }));
    }
    replace(meta, ...parts);
  }

  function renderBanner(): void {
    if (vote.status === "decided") {
      const winner = vote.choices.find((choice) => choice.id === vote.leader_choice_id);
      if (vote.outcome === "ordered" && vote.order_id && winner) {
        banner.replaceChildren(
          el(
            "div",
            { class: "notice", role: "status" },
            el("p", { text: t.t("vote.decided.ordered", { name: winner.restaurant_name }) }),
            el("a", {
              class: "button button-primary",
              href: `/orders/${vote.order_id}`,
              text: t.t("vote.go_to_order"),
            }),
          ),
        );
        return;
      }
      banner.replaceChildren(
        el("p", {
          class: "notice",
          role: "status",
          text: vote.outcome === "too_late" ? t.t("vote.decided.too_late") : t.t("vote.decided.no_ballots"),
        }),
      );
      return;
    }
    banner.replaceChildren(
      voteOpen(vote)
        ? el("span", { hidden: true })
        : el("p", { class: "notice", role: "status", text: t.t("vote.closing") }),
    );
  }

  function renderList(): void {
    const open = voteOpen(vote);
    const signedIn = app.session.isAuthenticated;
    const rows = vote.choices.map((choice, index) => choiceRow(choice, index, open && signedIn));

    const parts: Child[] = [
      el(
        "fieldset",
        { class: "vote-list" },
        el("legend", { class: "vote-legend", text: t.t("vote.your_choice") }),
        ...rows,
      ),
    ];

    if (!signedIn && open) {
      parts.push(el("a", { class: "link", href: loginHref(currentPath()), text: t.t("vote.login") }));
    }
    if (signedIn && open) {
      const actionsRow: Child[] = [];
      if (mine !== null) {
        actionsRow.push(button({ label: t.t("vote.withdraw"), onclick: () => void withdraw() }));
      }
      const mayAdd = vote.open_choices || isCreator() || app.session.isAdmin;
      if (mayAdd && vote.choices.length < 5) {
        actionsRow.push(button({ label: t.t("vote.add_restaurant"), onclick: () => void addRestaurant() }));
      }
      if (actionsRow.length > 0) {
        parts.push(el("div", { class: "actions" }, ...actionsRow));
      }
    }
    parts.push(status.element);
    rerender(left, section(t.t("vote.restaurants"), ...parts));
  }

  function choiceRow(choice: VoteChoice, index: number, enabled: boolean): HTMLElement {
    const ahead = choice.id === vote.leader_choice_id;
    const radio = el("input", {
      type: "radio",
      name: `vote-${vote.id}`,
      class: "vote-radio",
      value: choice.id,
      checked: choice.id === mine,
      disabled: !enabled,
      "data-key": `choice-${choice.id}`,
      onchange: () => void cast(choice.id),
    });

    const name: Child[] = [el("span", { class: "vote-name", text: choice.restaurant_name })];
    if (ahead) {
      name.push(
        el("span", {
          class: "badge badge-open",
          text: vote.status === "decided" ? t.t("vote.winner") : t.t("vote.ahead"),
        }),
      );
    }

    const row = el(
      "div",
      { class: `vote-row${ahead ? " vote-row-ahead" : ""}` },
      el("label", { class: "vote-choice" }, radio, logo(choice), ...name),
      el("span", { class: `vote-key vote-key-${index + 1}`, "aria-hidden": "true" }),
      el("a", {
        class: "link vote-menu",
        href: `/restaurants/${choice.restaurant_id}`,
        target: "_blank",
        rel: "noopener",
        text: t.t("nav.restaurants"),
      }),
      el("span", { class: "vote-count", text: String(choice.count) }),
    );

    const removable =
      voteOpen(vote) &&
      choice.count === 0 &&
      vote.choices.length > 2 &&
      me() !== undefined &&
      (isCreator() || choice.added_by_id === me() || app.session.isAdmin);
    if (removable) {
      row.appendChild(
        iconButton("trash", t.t("vote.remove_restaurant", { name: choice.restaurant_name }), "button-danger", () => {
          void removeRestaurant(choice);
        }),
      );
    }

    if (choice.voters && choice.voters.length > 0) {
      row.appendChild(
        el("p", {
          class: "muted vote-voters",
          text: t.t("vote.voters", { names: choice.voters.map((voter) => voter.name).join(", ") }),
        }),
      );
    }
    return row;
  }

  function logo(choice: VoteChoice): HTMLElement {
    if (!choice.restaurant_logo_image_id) {
      return el("span", { class: "vote-logo tile-logo-fallback" }, logoMark({}));
    }
    return el("img", { class: "vote-logo", src: thumbnailURL(choice.restaurant_logo_image_id), alt: "" });
  }

  function renderChart(): void {
    right.replaceChildren(
      el(
        "figure",
        { class: "vote-figure" },
        pieChart(
          vote.choices.map((choice, index) => ({
            value: choice.count,
            colour: index + 1,
            pulled: choice.id === vote.leader_choice_id,
          })),
          t.t("vote.no_votes"),
        ),
        el("figcaption", { class: "vote-total", text: t.t("vote.votes", { count: vote.total }) }),
      ),
    );
  }

  /** Takes a new state of the vote, keeping the viewer's own ballot. */
  function accept(next: Vote, own?: string | null): void {
    vote = next;
    if (own !== undefined) {
      mine = own;
    } else if (next.my_choice_id !== undefined) {
      mine = next.my_choice_id;
    }
    // A choice that went away takes the ballot with it.
    if (mine !== null && !vote.choices.some((choice) => choice.id === mine)) {
      mine = null;
    }
    if (vote.leader_choice_id !== leader) {
      leader = vote.leader_choice_id;
      const ahead = vote.choices.find((choice) => choice.id === leader);
      if (ahead) {
        app.announce(t.t("vote.ahead.announce", { name: ahead.restaurant_name }));
      }
    }
    render();
  }

  async function refresh(): Promise<void> {
    try {
      accept(await api.get<Vote>(`/votes/${id}`));
    } catch {
      // Reported by the next action; the page keeps what it has.
    }
  }

  async function cast(choiceID: string): Promise<void> {
    await runAction({ status, t }, async () => {
      const next = await api.put<Vote>(`/votes/${id}/ballot`, { choice_id: choiceID });
      accept(next, choiceID);
    });
    // A refused ballot leaves the radios as they were.
    render();
  }

  async function withdraw(): Promise<void> {
    await runAction({ status, t }, async () => {
      accept(await api.delete<Vote>(`/votes/${id}/ballot`), null);
    });
  }

  async function addRestaurant(): Promise<void> {
    let restaurants: Restaurant[];
    try {
      restaurants = await getList<Restaurant>("/restaurants", "restaurants");
    } catch (error) {
      status.fail(errorMessage(t, error));
      return;
    }
    const offered = new Set(vote.choices.map((choice) => choice.restaurant_id));
    const available = restaurants
      .filter((restaurant) => !offered.has(restaurant.id))
      .sort((a, b) => a.name.localeCompare(b.name, app.language));
    if (available.length === 0) {
      status.say(t.t("vote.no_more_restaurants"));
      return;
    }
    const choice = select(available.map((restaurant) => ({ value: restaurant.id, label: restaurant.name })));
    openFormDialog({
      t,
      title: t.t("vote.add_restaurant"),
      fields: [field({ label: t.t("restaurant.data"), control: choice })],
      submitLabel: t.t("vote.add_restaurant"),
      onSubmit: async () => {
        accept(await api.post<Vote>(`/votes/${id}/choices`, { restaurant_id: choice.value }));
      },
    });
  }

  async function removeRestaurant(choice: VoteChoice): Promise<void> {
    await runAction({ status, t }, async () => {
      accept(await api.delete<Vote>(`/votes/${id}/choices/${choice.id}`));
    });
  }

  function editVote(): void {
    const decisionAt = input({ type: "datetime-local", value: localInputValue(vote.decision_at), required: true });
    const deadlineAt = input({ type: "datetime-local", value: localInputValue(vote.deadline_at), required: true });
    const fulfilmentAt = input({ type: "datetime-local", value: localInputValue(vote.fulfilment_at), required: true });
    const problem = fieldError(decisionAt);
    const openChoices = checkbox(t.t("vote.open_choices"), { checked: vote.open_choices });
    const publicBallots = checkbox(t.t("vote.public_ballots"), { checked: vote.public_ballots });

    openFormDialog({
      t,
      title: t.t("vote.edit"),
      fields: [
        field({ label: t.t("vote.decision.label"), control: decisionAt, error: problem }),
        field({ label: t.t("order.deadline.label"), control: deadlineAt }),
        field({ label: t.t("order.fulfilment.label"), control: fulfilmentAt }),
        openChoices,
        publicBallots,
      ],
      onSubmit: async () => {
        const message = timesProblem(app, decisionAt.value, deadlineAt.value, fulfilmentAt.value);
        if (message) {
          problem.set(message);
          return false;
        }
        problem.clear();
        accept(
          await api.patch<Vote>(`/votes/${id}`, {
            decision_at: new Date(decisionAt.value).toISOString(),
            deadline_at: new Date(deadlineAt.value).toISOString(),
            fulfilment_at: new Date(fulfilmentAt.value).toISOString(),
            open_choices: (openChoices.querySelector("input") as HTMLInputElement).checked,
            public_ballots: (publicBallots.querySelector("input") as HTMLInputElement).checked,
          }),
        );
        return true;
      },
    });
  }

  async function deleteVote(): Promise<void> {
    const agreed = await confirmDialog({
      t,
      message: t.t("vote.confirm_delete"),
      ...(vote.total > 0 ? { detail: t.t("vote.confirm_delete.voters", { count: vote.total }) } : {}),
      confirmLabel: t.t("action.delete"),
    });
    if (!agreed) {
      return;
    }
    await runAction({ status, t }, async () => {
      await api.delete(`/votes/${id}`);
      app.navigate("/");
    });
  }

  const handlers = {
    onEvent: (name: string, data: unknown): void => {
      if (name === EVENT_VOTE_DELETED) {
        app.announce(t.t("vote.deleted"));
        left.replaceChildren(section(voteTitle(app, vote), el("p", { text: t.t("vote.deleted") })));
        right.replaceChildren();
        heading.replaceChildren();
        stream.close();
        return;
      }
      if (name === EVENT_VOTE_UPDATED && data !== null && typeof data === "object") {
        accept(data as Vote);
        return;
      }
      if (name === EVENT_VOTE_DECIDED) {
        void refresh();
      }
    },
    onReconnect: (): void => {
      void refresh();
    },
  };

  let stream = subscribeToVote(id, handlers, options.factory);
  const unsubscribe = app.session.subscribe(() => {
    stream.close();
    stream = subscribeToVote(id, handlers, options.factory);
    void refresh();
  });
  options.cleanup?.onCleanup(() => {
    stream.close();
    unsubscribe();
  });

  render();

  return pageWithActions(
    voteTitle(app, vote),
    heading,
    meta,
    banner,
    el("div", { class: "order-layout" }, left, right),
  );
}

/**
 * What is wrong with a vote's three times, or null (F12.1): the decision in
 * the future, before the deadline, before the food. Shared by the creation
 * page and the editor, so both say the same thing the server would.
 */
export function timesProblem(app: App, decision: string, deadline: string, fulfilment: string): string | null {
  const d = new Date(decision).getTime();
  const c = new Date(deadline).getTime();
  const f = new Date(fulfilment).getTime();
  if ([d, c, f].some((value) => Number.isNaN(value))) {
    return app.t.t("error.1002");
  }
  if (d <= Date.now() || d >= c) {
    return app.t.t("error.1018");
  }
  if (c >= f) {
    return app.t.t("order.deadline_before_fulfilment");
  }
  return null;
}
