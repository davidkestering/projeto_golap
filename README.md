# CuboDW

Motor OLAP (estilo **Mondrian**) escrito em **Go**, mais — em fase posterior — uma
aplicação visual de cubos com drag-and-drop (estilo **Saiku**). Implantação em
**microserviços Docker**.

Autor: **David Kestering** · Licença: Apache-2.0

> Plano completo e roteiro por fases: `~/.claude/plans/quero-sua-ajuda-eu-bubbly-sonnet.md`.

## 📖 Manual técnico

Manual HTML detalhado de todos os módulos, API REST e cobertura MDX, em 3 idiomas
(abra no navegador):

- [`DOCS/index.html`](DOCS/index.html) — seletor de idioma
- 🇧🇷 [`DOCS/manual.pt-br.html`](DOCS/manual.pt-br.html) · 🇬🇧 [`DOCS/manual.en.html`](DOCS/manual.en.html) · 🇪🇸 [`DOCS/manual.es.html`](DOCS/manual.es.html)

## Estado atual — Fases 0–8 (+ Fase 9 em progresso)

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

**Fase 5 (funções de conjunto + membros calculados):**
- `internal/service/mdxeval` refatorado para **resolver as posições de cada eixo
  independentemente** (no contexto do slicer), suportando `Order`,
  `TopCount`/`BottomCount` e `Filter` (ordenação/limite/filtro por medida).
- `internal/service/mdxeval/calc.go` — **membros calculados** (`WITH MEMBER`):
  avaliador numérico/booleano sobre medidas (ex.: `Profit AS [Store Sales] - [Store Cost]`,
  `Filter(set, [Unit Sales] > 1000)`), computado em Go sobre as medidas-base.

Exemplo:

```sh
curl -s -X POST localhost:8088/saiku/api/mdx/execute -H 'Content-Type: application/json' -d '{
  "mdx":"WITH MEMBER [Measures].[Profit] AS [Measures].[Store Sales] - [Measures].[Store Cost] SELECT {[Measures].[Profit]} ON COLUMNS, TopCount([Store].[Store State].Members,2,[Measures].[Unit Sales]) ON ROWS FROM [Sales]"
}'
```

**Fase 6 (mais funções MDX):**
- Operações de conjunto, **componíveis** (`resolveMemberSet` recursivo): `Union`,
  `Except`, `Intersect`, `Head`, `Tail`, `Distinct`, `Hierarchize`
  (ex.: `Head(Order([Store].[Store State].Members,[Unit Sales],BDESC), 2)`).
- Funções escalares em membros calculados: `IIf(cond, x, y)`, `CoalesceEmpty(...)`.

**Fase 7 (agregação sobre conjuntos + NON EMPTY):**
- **`Sum`/`Avg`/`Count`/`Aggregate` sobre conjuntos** em membros calculados —
  habilita **% do total / participação** (ex.: `[Pct] AS [Unit Sales] /
  Sum([Store].[Store State].Members, [Unit Sales]) * 100`). O subtotal é
  pré-computado no **contexto correto** (agrupado pelas demais dimensões do grid).
- **`NON EMPTY`** honrado: poda posições cujas células são todas vazias.

**Fase 8 (mostrar todos os membros vs NON EMPTY):**
- Enumeração de membros via **tabela de dimensão** (`sql.BuildLevelMembers`:
  `SELECT DISTINCT` com filtros de ancestrais/restrição), não pelo fato. Assim
  `[Dim].[Nível].Members` mostra **todos** os membros (inclusive sem fatos →
  células vazias) e `NON EMPTY` poda os vazios.

Agregações sobre conjuntos em calc: `Sum`/`Avg`/`Count`/`Aggregate` e também
**`Min`/`Max`** (estes via agregação por membro + redução em Go).

Também suportados: **named sets** (`WITH SET`), **ranges** (`m1 : m2`),
**múltiplas hierarquias** por dimensão (ex.: `[Time].[Weekly].[Week]`) e
**parent-child** plano (ex.: `[Employees].[Employee Id].Members` exibido por nome).

**Múltiplos dialetos SQL** (`internal/engine/sql/dialect.go`): a geração de SQL é
dirigida por um `Dialect` (quoting, placeholders, casts, `IN`, `LIMIT`/`TOP`).
Implementados e testados (golden) **PostgreSQL, MySQL/MariaDB, DuckDB e SQL Server**.
> Observação: o runtime executa via **pgx (PostgreSQL)**. Apontar para MySQL/DuckDB/
> SQL Server requer plugar o driver respectivo + carregar o FoodMart nesse banco
> (passo de deploy); a camada de geração de SQL já está pronta para eles.

Ainda **não** suportados (erro claro): mostrar todos os membros em `CrossJoin`
(multi-binding via fato), snowflake, navegação de árvore/rollup recursivo em
parent-child.

**Fase 9.1 (AI Query API):** surface tipada para agentes/LLMs consultarem cubos
**sem MDX** (`internal/web/ai.go`):

```
GET  /saiku/api/ai/cubes               # cubos + defaultMeasure + measureCount
GET  /saiku/api/ai/schema/{cube}       # auto-descritivo: medidas, dims, níveis com
                                       #   membros de amostra REAIS + exemplo de request
POST /saiku/api/ai/query               # {cube, measures, rows, columns, filters} (sem MDX)
```

Validação com auto-correção: nome inválido devolve `{status, field, value, available:[…]}`.

**Fase 9.2 (drill-through + totais):**
- `POST /saiku/api/query/drillthrough` — `{cube, filters, maxrows}` → linhas de fato
  cruas por trás de um contexto (`sql.BuildDrillthrough`).
- `POST /saiku/api/query` com `"totals":true` → acrescenta uma linha de total geral.

**Fase 9.3 (inteligência de tempo):** navegação sobre membros ordenados de um nível
(`internal/service/mdxeval/time.go`):
- `[m].PrevMember` / `[m].NextMember`, `[m].Lag(n)` / `[m].Lead(n)` — membro deslocado.
- `YTD([Time].[1997].[Q3])` — do início do ciclo até o membro (acumulado no ano).
  Ex.: `Lead(1)` de Q3 → Q4; `YTD(Q3)` → Q1, Q2, Q3.

**Fase 9.4 (cache de resultados):** cache em memória (FIFO) no nível do `queryexec.Run`,
indexado por SQL+args — beneficia query JSON, MDX e drill-through. Ligado por
`CUBODW_CACHE_SIZE` (default 256; 0 desabilita).
- `GET /saiku/api/cache` — métricas `{enabled, hits, misses, size, hitRatio}`.
- `POST /saiku/api/cache/clear` — esvazia. Stats também em `/saiku/api/info`.

**Trilha B — aplicação visual (UI Fases 1–5):** SPA leve embutida no binário
(`go:embed`, **sem toolchain Node**) servida em **`/ui/`** (`internal/web/ui/`):
- **Construtor drag-and-drop:** seletor de cubo → arrastar dimensões/níveis para
  Linhas/Colunas, medidas para Medidas, filtros (com membros) → Executar.
- **Cross-tab** (pivot linhas × colunas × medidas, no cliente).
- **Gráficos** de barras e linhas (canvas nativo, sem libs).
- **Drill-through**: clique numa célula → linhas de fato cruas num modal.
- **Salvar/abrir consultas** (localStorage).
- **Editor MDX**: escrever, **Validar** (`/mdx/parse`) e **Executar**
  (`/mdx/execute`), além de **gerar MDX a partir do construtor**.
- **Formatação de medidas** (formatString do schema), **exportar CSV**,
  **ordenar** clicando no cabeçalho e **paginação** (tabela achatada).

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
# 👉 UI drag-and-drop:  http://localhost:8088/ui/
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
    mdxeval/          avaliador MDX -> CellSet (Order/TopCount/Filter, set ops, WITH MEMBER)
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
