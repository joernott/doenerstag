// The order's chat (F11): what the people in an order say to each other about
// it, in the second tab of the menu column. Part of the order page.
//
// The lines come from their own route, which only answers the order's
// participants. The stream announces a new line only as a new count, because
// it cannot tell a participant from anybody else signed in (ADR-0012); this
// module fetches the lines when it hears one.

import { getList, api } from "../../api";
import { el, replace } from "../../dom";
import { formatChatTime } from "../../format";
import { button, input } from "../../components/forms";
import { runAction, statusLine } from "../../components/status";
import type { OrderMessage } from "../../types";
import type { OrderContext } from "./context";

/** The longest message the server takes, from migration 000017. */
export const MAX_MESSAGE_LENGTH = 1000;

/**
 * How close to the bottom counts as reading the newest line. Within this, a
 * new message scrolls into view; further up, somebody is reading back and the
 * list is left where they put it.
 */
const FOLLOW_SLACK = 24;

export interface OrderChat {
  /** The tab's panel: the messages above, the input below. */
  readonly panel: HTMLElement;
  /** How many messages there are, as far as this page knows. */
  count(): number;
  /** Whether this visitor may read and write here, and if not, why not. */
  unavailable(): string | undefined;
  /** The stream says the chat has grown to `count` lines. */
  heard(count: number): Promise<void>;
  /** Reads the messages again, if this visitor may. */
  reload(): Promise<void>;
}

export function orderChat(ctx: OrderContext): OrderChat {
  const { app, id } = ctx;
  const { t } = app;
  const status = statusLine();

  let messages: OrderMessage[] | null = null;
  let heardCount: number | null = null;

  // A log, so a screen reader reads a new line out as it arrives, and focusable
  // so the arrow keys scroll it without a pointer.
  const log = el("div", {
    class: "chat-log",
    role: "log",
    "aria-live": "polite",
    "aria-label": t.t("chat.log"),
    tabindex: "0",
  });

  const field = input({ name: "message", autocomplete: "off" });
  field.maxLength = MAX_MESSAGE_LENGTH;
  field.setAttribute("aria-label", t.t("chat.input"));
  const send = button({ label: t.t("chat.send"), variant: "primary", type: "submit" });

  // A form, so Enter in the field sends, as the button does.
  const compose = el("form", {
    class: "chat-compose",
    novalidate: true,
    onsubmit: (event: Event) => {
      event.preventDefault();
      void post();
    },
  });
  compose.append(field, send);

  const panel = el("div", { class: "chat" }, log, compose, status.element);

  function may(): boolean {
    return ctx.isParticipant();
  }

  function count(): number {
    return messages?.length ?? heardCount ?? ctx.detail()?.message_count ?? 0;
  }

  function unavailable(): string | undefined {
    if (!app.session.isAuthenticated) {
      return t.t("chat.login");
    }
    return may() ? undefined : t.t("chat.participants_only");
  }

  async function reload(): Promise<void> {
    if (!may()) {
      return;
    }
    await runAction({ status, t }, async () => {
      messages = await getList<OrderMessage>(`/orders/${id}/messages`, "messages");
      heardCount = null;
      render();
    });
  }

  async function heard(next: number): Promise<void> {
    heardCount = next;
    if (messages !== null && messages.length === next) {
      return;
    }
    await reload();
  }

  async function post(): Promise<void> {
    const text = field.value.trim();
    if (text === "") {
      field.focus();
      return;
    }
    const sent = await runAction({ status, t, busy: send }, async () => {
      await api.post(`/orders/${id}/messages`, { body: text });
    });
    if (sent) {
      field.value = "";
      // Somebody who has just written something wants to see it, wherever
      // they had scrolled to.
      await reload();
      log.scrollTop = log.scrollHeight;
    }
    field.focus();
  }

  function render(): void {
    const following = log.scrollHeight - log.scrollTop - log.clientHeight <= FOLLOW_SLACK;

    if (!messages || messages.length === 0) {
      replace(log, el("p", { class: "muted", text: t.t("chat.empty") }));
      return;
    }
    // Text nodes throughout: a message is what somebody typed, never markup.
    replace(
      log,
      ...messages.map((message) =>
        el(
          "p",
          { class: "chat-line" },
          el("strong", { text: message.user_name }),
          ` (${formatChatTime(app.language, message.created_at)}): ${message.body}`,
        ),
      ),
    );
    if (following) {
      log.scrollTop = log.scrollHeight;
    }
  }

  render();
  void reload();

  return { panel, count, unavailable, heard, reload };
}
