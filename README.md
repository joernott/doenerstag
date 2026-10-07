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

**0.4.0.** Everything the specification describes is built: accounts and
sessions, restaurants with menus and opening hours, orders with a deadline and a
pickup or delivery time, per-person items, a summary page for whoever is calling
the restaurant, live updates over SSE, German, English and French, and an
administration surface.

New since 0.3.0, from using it for real orders:

- **French**, as a third language, with the plural forms French has and German
  and English do not.
- **Both jobs can be claimed and given up.** "Me!" now sits beside collecting
  the money as well as fetching the food, and an × takes somebody off either
  job — for that person, whoever opened the order, and the administrator.
- **Whoever collects the money can do it from the summary.** The collector and
  the person fetching the food may open it without having ordered anything; the
  paid tick sits on the order page too, and the totals split into unpaid and
  paid.
- **A dish can be corrected from the order page**, with the same editor the
  restaurant page uses. Lines already ordered keep the price they were added
  with.
- **The administrator can set a password directly**, for somebody with no
  access to their mail, as well as sending a reset link.
- Smaller things: order titles in UTC beside times in local time, Save doing
  nothing in the availability dialogs, a language list unreadable under the
  pointer in the dark theme.

Still a 0.x release, and marked as a pre-release for the reason it was at
0.1.0: it works, and not many people have used it yet.

## Install

Four ways, all from the same source with the same version stamp:

```sh
# Debian and Ubuntu
apt install ./doenerstag_0.4.0-1_amd64.deb

# RHEL, Fedora, Rocky, Alma, openSUSE
dnf install ./doenerstag-0.4.0-1.x86_64.rpm

# Container
docker pull docker.io/joernott/doenerstag:0.4.0

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
