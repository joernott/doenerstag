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

Specification only. No code yet.

## Documentation

The specification lives in [docs/](docs/). Start with
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
| [adr/](docs/adr/) | Architecture decision records |

## Stack

Go backend, TypeScript and Tailwind frontend, PostgreSQL 18. A release is a
single binary with the frontend embedded.
