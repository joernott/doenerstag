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

**0.2.0.** Everything the specification describes is built: accounts and
sessions, restaurants with menus and opening hours, orders with a deadline and a
pickup or delivery time, per-person items, a summary page for whoever is calling
the restaurant, live updates over SSE, German and English, and an administration
surface.

New since 0.1.0, most of it from using the thing and finding out:

- **Forgotten passwords.** A reset link by mail, and the SMTP settings to send
  it with. An administrator can send the same link from the Users page or from
  the command line.
- **Command line administration.** `doenerstag user`, `doenerstag order` and
  `doenerstag restaurant`, the last of which exports a restaurant with its menu
  as YAML or JSON and imports it somewhere else — a menu is an hour of typing
  and should only be typed once. Every verb and flag is in
  [docs/15_cli_reference.md](docs/15_cli_reference.md).
- **A session the server has forgotten** no longer makes the application
  unreachable. It used to answer every request, the page included, with a JSON
  error until somebody cleared their cookies by hand.
- **Logging in returns you where you were** rather than to your account page.

Still a 0.x release, and marked as a pre-release for the reason it was at
0.1.0: it works, and not many people have used it yet.

## Install

Four ways, all from the same source with the same version stamp:

```sh
# Debian and Ubuntu
apt install ./doenerstag_0.2.0-1_amd64.deb

# RHEL, Fedora, Rocky, Alma, openSUSE
dnf install ./doenerstag-0.2.0-1.x86_64.rpm

# Container
docker pull docker.io/joernott/doenerstag:0.2.0

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
