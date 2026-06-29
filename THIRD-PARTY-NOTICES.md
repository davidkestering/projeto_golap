# Third-Party Notices

CuboDW (the original code in this repository) is licensed under the
**Apache License 2.0** (see `LICENSE`). This file lists third-party material
that is **either bundled in this repository or used as a reference**, together
with its own license and attribution.

---

## Bundled in this repository

### FoodMart demo schema and sample data — EPL-1.0

- Files:
  - `internal/demo/FoodMart.xml` (Mondrian schema, embedded as the default demo cube)
  - `deploy/postgres/initdb/10-foodmart.sql.gz` (FoodMart sample data, converted
    from the H2 dump shipped with Saiku to PostgreSQL by
    `deploy/postgres/convert_h2_to_pg.sh`)
- Origin: **Pentaho / Julian Hyde — Mondrian** FoodMart sample database.
- License: **Eclipse Public License v1.0 (EPL-1.0)** —
  https://www.eclipse.org/legal/epl-v10.html
- Copyright: © 2000–2002 Kana Software, Inc.; © 2002–2009 Julian Hyde and others.

These files remain under EPL-1.0. CuboDW's Apache-2.0 license does **not** relicense
them. The original license header is preserved in `FoodMart.xml`.

---

## Used as a reference (not redistributed here)

### Saiku Analytics — Apache-2.0

- The `saiku-4.5.2` distribution was used as a **reverse-engineering reference** to
  understand the OLAP service/REST/semantic-layer behaviour that CuboDW reimplements
  in Go. The Saiku source archive is **not** committed to this repository.
- Project: https://github.com/spiculedata/saiku
- License: **Apache License 2.0**.

### Mondrian (Spicule fork) — EPL-1.0

- The Mondrian source (`reference/mondrian`, a local clone, **not** committed) was
  used as the **specification reference** for the MDX grammar and OLAP engine
  behaviour that CuboDW reimplements in Go.
- Project: https://github.com/spiculedata/mondrian-saiku
- License: **Eclipse Public License v1.0 (EPL-1.0)**.

> CuboDW is an independent, clean-room-style reimplementation in Go. It does not
> copy Mondrian/Saiku source code; it reproduces documented behaviour and grammar.
> Only the FoodMart demo assets listed above are redistributed, under their own
> EPL-1.0 license.

---

## Go module dependencies

Build-time/runtime Go dependencies (see `go.mod`) are fetched from their upstream
modules under their own licenses (predominantly BSD/MIT/Apache-2.0), e.g.
`github.com/jackc/pgx`, `github.com/spf13/cobra`, `gopkg.in/yaml.v3`.
