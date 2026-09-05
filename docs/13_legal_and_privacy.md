# 13 Legal and privacy

**This document is not legal advice.** It records what the application does with
personal data and what the operator has to decide, so that whoever is
responsible — a data protection officer, a works council, a club committee — has
the facts in one place. Obligations depend on the jurisdiction and on the
organization, and the operator has to confirm them.

The application is designed for deployment in the EU, and specifically in
Germany, so GDPR and the German implementing rules are the reference.

## Who is who

| Role                | Who                                                                     |
| ------------------- | ----------------------------------------------------------------------- |
| Controller          | The organization operating the installation. Not the software's authors. |
| Processor           | None. No data leaves the installation.                                    |
| Data subjects       | Registered users, and anyone named in a free-text field.                  |
| Third-party recipients | None. The application makes no outbound network connections.           |

The software's authors never receive any data. There is no telemetry, no crash
reporting, no update check and no analytics. The application makes no outbound
connections at all at runtime.

## What personal data is processed

| Data                                    | Where                              | Why                                            |
| --------------------------------------- | ---------------------------------- | ---------------------------------------------- |
| User name                               | `app_user.name`                     | Login and identifying who ordered what.         |
| Display name                            | `app_user.display_name`             | Optional. Shown instead of the user name.       |
| E-mail address                          | `app_user.email`                    | Optional. Never used to send anything.          |
| Password hash                           | `app_user.password_hash`            | Authentication. Argon2id, not reversible.       |
| Last login timestamp                    | `app_user.last_login_at`            | Operational.                                    |
| IP address and user agent               | `session`, and the request log       | Session handling and troubleshooting.           |
| Food choices and modifications          | `order_item`                        | The purpose of the application.                 |
| Free-text notes                         | `order_item.note`, `food_order.money_collector`, `food_order.pickup_person` | Coordination. May name people. |
| Who created and last changed each row   | `created_by` / `updated_by`         | Accountability.                                 |
| Every data-changing action with the user | The INFO-level log                 | Audit trail.                                    |

### Two items that deserve attention

**Food choices can reveal special category data.** A person who consistently
filters out pork, or who only ever orders food marked without a given allergen,
is revealing something about their religion or their health — categories under
Art. 9 GDPR. The application does not ask for this and does not label it as
such, but the inference is available to anyone reading an order. The
countermeasure is not technical: it is that participation is voluntary and that
the group already sees what everyone eats at lunch. The operator should be aware
of the inference and should not, for example, feed the data into anything else.

**Order data is readable without logging in.** Anyone who can reach the server
sees who ordered what. That is intentional and is the application's purpose, but
it means the access control on the network is the access control on the data.
The operator must confirm the deployment really is confined to the intended
network.

## Legal basis

For the operator to determine. The plausible bases, in the German employment
context:

| Processing                          | Likely basis                                                          |
| ----------------------------------- | --------------------------------------------------------------------- |
| Account and order data              | Art. 6(1)(a) consent — registration is voluntary and the tool is for the user's own benefit. Art. 6(1)(f) legitimate interest is the alternative. |
| Session cookies, CSRF token         | Strictly necessary for a service the user requested — § 25(2) TDDDG, no consent banner required. |
| Language, theme and name cookies    | Set only as a direct result of the user's own choice. Same exemption in the operator's view; confirm. |
| Logs including IP addresses         | Art. 6(1)(f) legitimate interest in operating and securing the service. |

Because registration is voluntary and nothing is pre-populated from an HR
system, consent is the cleanest basis. The operator should make sure nobody is
pressured into using the tool.

## Data subject rights and how the application supports them

| Right                          | Article  | Support                                                                 |
| ------------------------------ | -------- | ----------------------------------------------------------------------- |
| Information                    | 13, 14   | The legal notes page. Content supplied by the operator.                  |
| Access                         | 15       | The user's own data is visible in the UI. The API returns it as JSON. There is no one-click export; the administrator can assemble one. |
| Rectification                  | 16       | The user page edits name, display name, e-mail and password. Order items are editable while the order is active. |
| Erasure                        | 17       | Self-service account deletion, immediate and irreversible. See below.    |
| Restriction                    | 18       | Not implemented. In practice erasure is the remedy offered.               |
| Portability                    | 20       | Not implemented as a feature. The API returns everything as JSON.        |
| Objection                      | 21       | Stop using the tool and delete the account.                              |

### What account deletion actually does

Specified in F2.6 of [02_features.md](02_features.md) and in
[03_data_model.md](03_data_model.md). Restated here because it is the erasure
mechanism:

- The `app_user` row is deleted immediately. Nothing is soft-deleted, nothing is
  kept "for a while".
- Sessions and API tokens cascade away.
- Order items in **active** orders are deleted outright.
- Order items in **expired** orders are reassigned to the deleted-user
  placeholder and shown as "deleted user". The food choice survives; the link to
  the person does not.
- Orders the user created are reassigned to the placeholder.
- `created_by` and `updated_by` references are set to null.

The reassignment is what makes the remaining data non-personal: an expired order
line reading "1x Döner, no onions — deleted user" identifies nobody. It is
removed entirely at the next `cleanup` run anyway.

Two residues the operator should know about:

1. **The log.** Log lines naming the user survive until the logs rotate away.
   The default `logrotate` configuration in [10_operations.md](10_operations.md)
   keeps 14 days. Erasure is therefore complete after at most that period, not
   instantly. Shorten the log retention if that is not acceptable.
2. **Free-text fields.** A note written by someone else — "for Anna" in a pickup
   person field — is not touched by that person's account deletion. There is no
   automated way to find it. The administrator can edit or delete the order.

Both should be stated in the privacy notice.

## Retention

| Data                    | Retained                                                          |
| ----------------------- | ----------------------------------------------------------------- |
| Orders and order items  | Until the deadline is older than `--retention`, default 14 days.   |
| Accounts                | Until the user or the administrator deletes them.                  |
| Sessions                | Until the idle or absolute timeout, then removed by `cleanup`.      |
| API tokens              | Until revoked or expired.                                          |
| Logs                    | Whatever `logrotate` is configured for. 14 days in the example.     |
| Backups                 | Whatever the operator's backup policy says. Not managed by the application. |

Retention is only real if `cleanup` actually runs. An installation without the
cron entry keeps every order forever. That makes scheduling `cleanup` a
compliance step, not just an operational one.

Backups are the other gap: a deleted account persists in every backup taken
before the deletion. The usual answer — backups are restored wholesale, not
mined, and age out on their own schedule — should be written into the operator's
policy rather than assumed.

## The two content pages

Both hold an HTML snippet supplied by the operator, loaded from a file at
install time and replaceable later by the administrator. See F9 in
[02_features.md](02_features.md).

### Imprint

An intranet application is not a "digital service" offered to the public, so the
imprint obligation in § 5 DDG generally does not apply. The page exists anyway
because it is the natural place for "who runs this thing, and who do I complain
to". Useful content:

- The operating organization and department.
- A responsible person and how to reach them.
- Where to report a bug or ask for help.

If the installation ever becomes reachable from the public internet, the § 5 DDG
obligations apply in full and the page has to be revisited.

### Legal notes

This is where the privacy notice goes. To satisfy Art. 13 GDPR it needs:

- Identity and contact details of the controller.
- Contact details of the data protection officer, if one is appointed.
- The purposes and the legal basis for each processing activity.
- The legitimate interests relied on, where that is the basis.
- Recipients — for this application, none.
- Retention periods, or the criteria for them.
- The data subject rights listed above, including the right to withdraw consent
  and the right to complain to a supervisory authority.
- Whether providing the data is required and what happens if it is not.
- Confirmation that there is no automated decision-making and no profiling.

Other things that plausibly belong on this page:

- A note that participation is voluntary.
- A note that food choices are visible to everyone who can reach the server, and
  that this may allow inferences about diet, religion or health.
- The two erasure residues named above: log retention and free-text mentions.
- Any acceptable-use rules the organization wants to impose.
- The software's own licence and the third-party licences it ships.

### Both pages accept HTML

The content is sanitized on write with an allow-list sanitizer and constrained
on read by the CSP (see [05_auth_and_permissions.md](05_auth_and_permissions.md)).
Scripts, event handlers and `javascript:` URLs are stripped. Only the
administrator can replace either page.

## Operator checklist

Before putting an installation in front of real people:

- [ ] Confirm the deployment is confined to the intended network.
- [ ] Write and install the imprint snippet.
- [ ] Write and install the legal notes snippet with a complete Art. 13 privacy
      notice.
- [ ] Decide the legal basis and record it.
- [ ] Add the application to the record of processing activities under Art. 30,
      if the organization keeps one.
- [ ] In Germany, and where an installation is used in an employment context,
      check with the works council. A system that records what individual
      employees do can be subject to co-determination under § 87(1) no. 6 BetrVG,
      even when monitoring is plainly not its purpose. Getting this agreed early
      is much easier than retrofitting it.
- [ ] Decide the retention period and schedule `cleanup`.
- [ ] Decide the log retention period and configure `logrotate`.
- [ ] Decide the backup retention period and how erasure requests interact with
      it.
- [ ] Decide who holds the `root` password and who acts on erasure requests.
- [ ] Confirm nobody is required to use the tool.

## Other legal matters

**Restaurant logos.** Users upload logos that are usually trademarked and
copyrighted. Using them to identify the restaurant whose food is being ordered
is ordinarily fine, but the operator should remove any logo a restaurant objects
to. There is no fair-use claim being asserted here — it is simply low-risk in
context.

**Menu data.** Prices and dish names are facts and not protected. Descriptions
copied verbatim from a restaurant's own menu may be. A note in the "add menu
item" form asking users to write their own descriptions is cheap and worth
having.

**The application's own licence** should be chosen before the first release and
recorded in a `LICENSE` file at the repository root. The licences of the
dependencies listed in [08_technologies.md](08_technologies.md) must be
compatible with it and should be shipped — a generated `THIRD_PARTY_LICENSES`
file, reachable from the legal notes page.

**No payment data** is ever processed. The application records who owes what;
money changes hands outside it. This keeps it clear of PSD2 and PCI DSS
entirely, and that is a deliberate scope decision worth preserving.
