# CuboDW

Motor OLAP (estilo **Mondrian**) escrito em **Go**, mais — em fase posterior — uma
aplicação visual de cubos com drag-and-drop (estilo **Saiku**). Implantação em
**microserviços Docker**.

> Plano completo e roteiro por fases: `~/.claude/plans/quero-sua-ajuda-eu-bubbly-sonnet.md`.

## Estado atual — Fases 0–4

**Fase 0 (infra):**
- `cmd/cubodw` — CLI (cobra): `serve-engine`, `healthcheck`, `version`.
- `internal/web` — servidor HTTP: `/health`, `/ready` (pinga Postgres), `/saiku/api/info`.
- `deploy/` — Dockerfile multi-stage + `docker-compose.yml` (engine + `postgres:16`
  com FoodMart auto-carregado via `initdb`).

**Fase 1 (metadados + descoberta):**
- `internal/engine/metadata` — IR (Schema/Cube/Dimension/Hierarchy/Level/Measure)
  com nomes únicos MDX.
- `internal/engine/schema/mondrian` — import de **Mondrian XML v3** → IR
  (resolve `DimensionUsage`, dimensões inline, `MeasureExpression`).
- `internal/engine/schema/yaml` — loader do formato de **autoria YAML** → IR.
- `internal/demo` — schema **FoodMart** embutido (cubo padrão).
- `internal/service/discover` + `internal/web/discover.go` — **API de descoberta**.

Endpoints de descoberta (shape compatível com Saiku):

```
GET /saiku/api/discover                                                 # árvore connection→catalog→schema→cubes
GET /saiku/api/discover/{conn}/{catalog}/{schema}/{cube}/metadata       # dimensões+hierarquias+níveis e medidas
GET /saiku/api/discover/{conn}/{catalog}/{schema}/{cube}/dimensions     # só dimensões
```

**Fase 2 (star schema + SQL):**
- `internal/engine/query` — spec de query (medidas, níveis em linhas/colunas, filtros) + Result tipado.
- `internal/engine/sql` — dialeto Postgres + gerador de `SELECT/JOIN/WHERE/GROUP BY`
  (resolve star schema por FK=PK; erro claro p/ snowflake).
- `internal/service/queryexec` — executa a SQL no Postgres e materializa records tipados.

Endpoints de query:

```
POST /saiku/api/query           # executa: {cube, rows[], columns[], measures[], filters[]} -> records
POST /saiku/api/query/preview   # valida + gera SQL sem executar
```

Exemplo (números canônicos do FoodMart):

```sh
curl -s -X POST localhost:8088/saiku/api/query -H 'Content-Type: application/json' -d '{
  "cube":"Sales",
  "rows":[{"dimension":"Time","level":"Year"}],
  "measures":["Unit Sales","Store Sales"]
}'
# 1997 -> Unit Sales 266773, Store Sales 565238.13
```

**Fase 3 (parser MDX):**
- `internal/engine/mdx` — lexer + AST + parser recursivo-descendente, fiel ao
  `MdxParser.jj` do Mondrian (precedência de operadores, `WITH MEMBER/SET`, eixos
  `NON EMPTY ... ON COLUMNS/ROWS/AXIS(n)`, sets `{}`, tuplas `()`, funções/métodos,
  `.props`, `CASE`, membros compostos como `[Measures].[Unit Sales]`).
- `POST /saiku/api/mdx/parse` — `{mdx}` → AST (eixos, slicer, fórmulas, forma canônica).

**Fase 4 (avaliador MDX → CellSet):**
- `internal/service/mdxeval` — reduz o AST MDX ao modelo de query da Fase 2
  (níveis + medidas + filtros), executa e **pivota** os records num CellSet.
  Suporta: medidas em eixo ou medida default (slicer/cubo), `{membros}`,
  `[Dim].[Nível].Members`, `[membro].Children`, `CrossJoin`, tuplas e `WHERE`.
- `POST /saiku/api/mdx/execute` — `{mdx}` → CellSet (eixos, células e `grid` 2D).

Exemplo:

```sh
curl -s -X POST localhost:8088/saiku/api/mdx/execute -H 'Content-Type: application/json' -d '{
  "mdx":"SELECT {[Measures].[Unit Sales],[Measures].[Store Sales]} ON COLUMNS, [Store].[Store Country].Members ON ROWS FROM [Sales]"
}'
# USA -> 266773 | 565238.13
```

Ainda **não** suportados (erro claro): `Filter`/`Order`/`TopCount`, membros
calculados (`WITH`), NON EMPTY explícito, snowflake. → próximas fases.

Schema carregado via `CUBODW_SCHEMA` (`.xml` Mondrian | `.yml`/`.yaml` autoria);
vazio usa o FoodMart embutido.

> Engine exposto em **localhost:8088** (host 8088 → container 8080, para não
> conflitar com a porta 8080 já usada no host). Postgres em localhost:5432
> (user/pass `cubodw`, db `foodmart`).

Próximas fases (`internal/engine/...`: star/SQL, MDX parser, calc/avaliador,
cache, result) — ver o plano.

## Pré-requisitos

Apenas **Docker** + **Docker Compose** no host. Go e Java **não** são necessários
(build do Go roda em container; o Saiku Java de referência roda via imagem Docker).

## Como rodar

```sh
# resolver dependências (gera go.sum)
make tidy

# compilar e testar (em container golang)
make build
make test

# subir a stack: engine + postgres:16 (FoodMart é auto-carregado no 1o boot)
make up
curl localhost:8088/health   # {"status":"ok"}
curl localhost:8088/ready    # {"status":"ready"} quando conectado ao Postgres
curl localhost:8088/saiku/api/info
make down
```

## Referência (engenharia reversa)

- `reference/mondrian/` — clone do fork Mondrian (Spicule), só leitura, fora do build.
- `saiku-4.5.2.zip` — fonte de engenharia reversa da camada Saiku (REST/AI/semântica).

## Layout

```
cmd/cubodw/        CLI
internal/
  config/          configuração via env
  version/         versão
  web/             servidor HTTP + DTOs/handlers de descoberta
  demo/            schema FoodMart embutido (cubo padrão)
  engine/
    metadata/      IR multidimensional
    schema/mondrian/  import Mondrian XML v3 -> IR
    schema/yaml/      autoria YAML -> IR
    query/            spec de query + Result tipado
    sql/              dialeto Postgres + gerador de SQL (star schema)
    mdx/              lexer + AST + parser MDX
    (próximas fases: Filter/Order/TopCount, WITH, aggcache, Arrow)
  service/
    discover/         descoberta de metadados
    queryexec/        execução de query no Postgres
    mdxeval/          avaliador MDX -> CellSet (reusa queryexec)
deploy/
  engine/Dockerfile
  docker-compose.yml
  postgres/convert_h2_to_pg.sh + initdb/  seed FoodMart
reference/mondrian/  spec de leitura do Mondrian
```

## Licença

O código do CuboDW é licenciado sob a **Apache License 2.0** — veja
[`LICENSE`](LICENSE). Você pode usar, modificar e comercializar, **mantendo o
crédito ao autor e ao projeto** (avisos de copyright + [`NOTICE`](NOTICE)).

Os dados/schema de demonstração **FoodMart** (`internal/demo/FoodMart.xml` e
`deploy/postgres/initdb/10-foodmart.sql.gz`) vêm do Mondrian e permanecem sob
**Eclipse Public License v1.0 (EPL-1.0)** — não são cobertos pela Apache-2.0.
Detalhes em [`THIRD-PARTY-NOTICES.md`](THIRD-PARTY-NOTICES.md).
