# doenerstag — a food ordering app

A web application for coordinating food orders in a group. One person opens an
order for a restaurant and a pickup time, everyone else adds what they want, and
a summary page tells whoever is calling the restaurant exactly what to say and
who owes what.

It does not take payments and does not place orders with restaurants. It
organizes them.

doenerstag is an intranet tool: it runs inside a company or club network where
everyone who can reach it is trusted, and it needs no internet access to run.

## Status

**0.7.0.** Everything the specification describes is built: accounts and
sessions, restaurants with menus and opening hours, orders with a deadline and a
pickup or delivery time, per-person items, a summary page for whoever is calling
the restaurant, a chat on every order, votes on where to order, live updates
over SSE, German, English and French, and an administration surface.

New in 0.7.0:

- **Vote on the restaurant.** The plus tile on the start page now asks
  whether to start an order or a vote. A vote offers two to five restaurants;
  everybody signed in picks one and may change their mind until the decision
  time. The vote page lists the restaurants with their counts beside a pie
  chart and the total, and highlights the one ahead. At the decision time the
  server makes the order for the winner -- with the vote's creator, deadline
  and fulfilment time, as a pickup the creator can switch to delivery -- or
  none, if nobody voted. On a tie, the restaurant that reached the count first
  wins.
- **Public or secret ballots**, chosen by whoever starts the vote. Secret
  means nobody is told who chose what, the administrator included; a vote
  somebody has voted in secretly cannot be made public afterwards. Visitors
  who are not signed in see the counts and no names.
- **Fresh dependencies**: Go 1.27.4 and every Go module at its newest release,
  `klauspost/compress` 1.20.2 for GO-2026-6865, Node.js 26, TypeScript 6,
  ESLint 10, Vitest 5, esbuild 0.28, and the GitHub Actions at their current
  majors.
- Accessible names: the logo link and a category's Edit and Availability
  buttons now start with the words they show, so voice control finds them.

One migration comes with it, so run `doenerstag update` after installing.

Still a 0.x release, and marked as a pre-release for the reason it was at
0.1.0: it works, and not many people have used it yet.

## Install

Four ways, all from the same source with the same version stamp:

```sh
# Debian and Ubuntu
apt install ./doenerstag_0.7.0-1_amd64.deb

# RHEL, Fedora, Rocky, Alma, openSUSE
dnf install ./doenerstag-0.7.0-1.x86_64.rpm

# Container
docker pull docker.io/joernott/doenerstag:0.7.0

# Anything else: the .tar.gz or the .zip from the release page
```

Then provision the database and write a configuration file:

```sh
cd /etc/doenerstag
DOENER_DATABASE_ROOT_PASSWORD='…' doenerstag install -o /etc/doenerstag/doenerstag.yaml
systemctl enable --now doenerstag
```

You need a PostgreSQL 18 server and a TLS certificate and key. The full
procedure, a working `docker compose` stack, backup, log rotation and
troubleshooting are in [docs/10_operations.md](docs/10_operations.md).

## Documentation

The documentation lives in [docs/](docs/). Start with
[docs/01_overview.md](docs/01_overview.md), which carries a map of the rest.

| Document | Contents |
| -------- | -------- |
| [01_overview.md](docs/01_overview.md) | Scope, deployment context, glossary |
| [02_features.md](docs/02_features.md) | Functional requirements |
| [03_data_model.md](docs/03_data_model.md) | Entities, constraints, seed data |
| [04_api.md](docs/04_api.md) | REST API |
| [05_auth_and_permissions.md](docs/05_auth_and_permissions.md) | Sessions, tokens, permission matrix |
| [06_ui_ux.md](docs/06_ui_ux.md) | Screens and accessibility |
| [07_i18n.md](docs/07_i18n.md) | Languages and formatting |
| [08_technologies.md](docs/08_technologies.md) | Stack, layout, build, logging |
| [09_configuration.md](docs/09_configuration.md) | CLI, environment and config file |
| [10_operations.md](docs/10_operations.md) | Install, update, backup, monitoring |
| [11_nonfunctional.md](docs/11_nonfunctional.md) | Security, performance, browsers |
| [12_testing.md](docs/12_testing.md) | Test strategy |
| [13_legal_and_privacy.md](docs/13_legal_and_privacy.md) | GDPR and legal obligations |
| [14_implementation_plan.md](docs/14_implementation_plan.md) | Sprint plan and task list |
| [15_cli_reference.md](docs/15_cli_reference.md) | Every verb and flag, generated from the command tree |
| [adr/](docs/adr/) | Architecture decision records |

## Stack

Go backend, TypeScript and Tailwind frontend, PostgreSQL 18. A release is a
single binary with the frontend embedded, published as a `.deb`, an `.rpm`, a
bare binary and a container image.

## Licence

BSD 3-Clause. See [LICENSE](LICENSE). The licences of the dependencies and the
obligations that follow from them are in
[docs/08_technologies.md](docs/08_technologies.md#licensing).
