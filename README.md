# CuboDW

Motor **OLAP** (estilo **Mondrian**) em **Go** com uma **aplicação visual de cubos**
drag-and-drop (estilo **Saiku**): você descreve cubos sobre um banco relacional e
consulta por **arrastar-e-soltar**, **MDX** ou **API REST**. Sobe inteiro em
**Docker** — só `docker compose`, sem instalar Go ou Java.

Autor: **David Kestering** · Licença: **Apache-2.0**

---

## 🚀 Subir em 1 minuto (a partir de um clone)

**Pré-requisitos:** apenas **Docker** + **Docker Compose**. (Go e Java **não**
precisam — o build do Go roda dentro de um container.)

```sh
git clone https://github.com/davidkestering/projeto_golap.git
cd projeto_golap
make up
```

O `make up` compila o engine e sobe a stack: **engine** + **PostgreSQL 16** com o
cubo de demonstração **FoodMart** carregado automaticamente no primeiro boot
(pode levar ~1 min na primeira vez). Quando terminar:

| O quê | Onde |
|---|---|
| 🖥️ **Interface visual** | **http://localhost:8088/ui/** — login demo **`admin` / `admin`** |
| ❤️ Saúde / prontidão | `curl localhost:8088/health` · `curl localhost:8088/ready` |
| ℹ️ Info do serviço | `curl localhost:8088/saiku/api/info` |
| 📖 Manual técnico | abra [`DOCS/index.html`](DOCS/index.html) no navegador |

Para derrubar: `make down`. (Sem `make` no host? Use
`docker compose -f deploy/docker-compose.yml up --build -d`.)

> O engine fica em **localhost:8088** (host 8088 → container 8080). O PostgreSQL
> fica em **localhost:5432** (user/senha `cubodw`, banco `foodmart`).

---

## 🧭 Por onde começar a usar e testar

### 1) Pela interface visual (recomendado)

1. Abra **http://localhost:8088/ui/** e faça login com **`admin` / `admin`**
   (ou clique em **Registrar** para criar uma conta comum).
2. No modo **Construtor**: escolha o **Cubo** (ex.: `Sales`) no topo.
3. **Arraste** medidas (verdes) e níveis (roxos) da barra esquerda para as zonas
   **Linhas / Colunas / Medidas / Filtros** e clique **▶ Executar**.
4. Explore: alterne **Tabela / Barras / Linhas**, clique numa célula de medida
   para o **drill-through**, use **ver SQL**, **⬇ CSV**, ordenar pelo cabeçalho,
   **💾 Salvar** consultas. O modo **MDX** deixa escrever/validar/executar MDX.

### 2) Pela API REST (terminal)

Com a autenticação ligada (padrão), faça login uma vez e reúse o cookie:

```sh
# 1) login -> guarda o cookie de sessão
curl -c cookies.txt -X POST localhost:8088/saiku/api/auth/login \
  -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin"}'

# 2) consulta tabular (modelo JSON, sem MDX)
curl -b cookies.txt -X POST localhost:8088/saiku/api/query \
  -H 'Content-Type: application/json' -d '{
    "cube":"Sales",
    "rows":[{"dimension":"Store","level":"Store State"}],
    "measures":["Unit Sales","Store Sales"]
  }'

# 3) ou MDX direto
curl -b cookies.txt -X POST localhost:8088/saiku/api/mdx/execute \
  -H 'Content-Type: application/json' -d '{
    "mdx":"SELECT {[Measures].[Unit Sales]} ON COLUMNS, [Store].[Store Country].Members ON ROWS FROM [Sales]"
  }'
```

Descobrir o que existe: `curl -b cookies.txt localhost:8088/saiku/api/discover`
(cubos) e `.../{conn}/{catalog}/{schema}/{cube}/metadata` (dimensões e medidas).
Há ainda a **AI Query API** (`/saiku/api/ai/cubes`, `/ai/schema/{cube}`,
`POST /ai/query`) — schema auto-descritivo e validação tipada, pensada para LLMs.

---

## 🧊 Criar e adicionar cubos novos

Você pode **criar um cubo e usá-lo na hora**, sem reiniciar o engine. Um cubo é
descrito por um *schema* em **YAML de autoria** (enxuto) ou **Mondrian XML**, que
liga dimensões/níveis/medidas a tabelas e colunas do seu banco.

### Pela interface (mais fácil)

1. Logado como **admin**, clique em **⬢ Cubos** no topo.
2. No modal, clique **Exemplo** (preenche um YAML pronto), ajuste, e use
   **Validar** (confere sem aplicar) → **Adicionar**.
3. O cubo novo aparece no seletor **Cubo** — selecione e monte sua consulta.

### Pela API

```sh
# valida sem aplicar (dry-run)
curl -b cookies.txt -X POST localhost:8088/saiku/api/schemas/validate \
  -H 'Content-Type: application/json' -d '{"content":"<seu YAML aqui>"}'

# adiciona (papel admin)
curl -b cookies.txt -X POST localhost:8088/saiku/api/schemas \
  -H 'Content-Type: application/json' -d '{"content":"<seu YAML aqui>"}'

curl -b cookies.txt localhost:8088/saiku/api/schemas                 # listar
curl -b cookies.txt -X DELETE localhost:8088/saiku/api/schemas/Inventory   # remover (admin)
```

### Formato YAML de um cubo

```yaml
schema: Inventory                  # nome do schema (grupo de cubos)
cubes:
  - name: WarehouseDemo            # nome do cubo (único no catálogo)
    fact: inventory_fact_1997      # tabela fato
    defaultMeasure: Units Shipped
    measures:
      - {name: Units Shipped,  column: units_shipped,  agg: sum}
      - {name: Warehouse Sales, column: warehouse_sales, agg: sum, format: "#,###.00"}
    dimensions:
      - name: Store
        foreignKey: store_id       # FK na fato
        table: store               # tabela de dimensão
        primaryKey: store_id       # PK na dimensão
        levels:
          - {name: Country, column: store_country}
          - {name: State,   column: store_state}
```

- As **tabelas/colunas referenciadas precisam existir no banco** conectado
  (acima usamos tabelas reais do FoodMart). Para apontar para o **seu** banco,
  ajuste o DSN em `CUBODW_PG_DSN` (ver Configuração).
- Nomes de cubo/schema são **normalizados**: viram **MAIÚSCULAS**, sem espaços
  nem caracteres especiais (tudo junto); nomes **repetidos** recebem sufixo
  incremental **V1**, **V2**, **V3**… — nunca há conflito (ex.: `Vendas 2024` →
  `VENDAS2024`; um segundo `Sales` → `SALESV1`).
- Também é aceito **Mondrian XML** (`{"content":"<Schema …>", "format":"xml"}`).
- O **formato completo** (XML e YAML, com hierarquias, tempo, parent-child) está
  no manual, seção *Formatos de schema*.

> **Persistência:** cubos adicionados em runtime valem para a sessão. Para que
> sobrevivam a um restart, defina `CUBODW_SCHEMAS_DIR` apontando para um diretório
> gravável (montado como volume) — o engine recarrega os schemas de lá na subida.
> Alternativa: aponte `CUBODW_SCHEMA` para um arquivo de schema fixo no boot.

---

## ⚙️ Configuração (variáveis de ambiente)

| Variável | Default | Descrição |
|---|---|---|
| `CUBODW_HTTP_ADDR` | `:8080` | Endereço HTTP (dentro do container) |
| `CUBODW_PG_DSN` | (no compose) | DSN do PostgreSQL. Vazio → `/ready` reporta `no-db` |
| `CUBODW_SCHEMA` | — | Schema fixo no boot (`.xml` Mondrian \| `.yml/.yaml` YAML). Vazio → FoodMart embutido |
| `CUBODW_SCHEMAS_DIR` | — | Dir gravável para **persistir** cubos adicionados em runtime |
| `CUBODW_CACHE_SIZE` | `256` | Nº de resultados em cache (FIFO). `0` desabilita |
| `CUBODW_AUTH_ENABLED` | `true` | Liga a autenticação + middleware de proteção |
| `CUBODW_AUTH_SECRET` | (dev) | Segredo HMAC que assina os cookies. **Defina em produção** |
| `CUBODW_USERS_FILE` | — | Persiste usuários em JSON (vazio = só memória) |
| `CUBODW_CONNECTION` | `foodmart` | Nome lógico da conexão na descoberta |

No `make up`, o `CUBODW_PG_DSN` já vem configurado no `deploy/docker-compose.yml`
apontando para o Postgres da stack — não precisa fazer nada para o demo funcionar.

---

## ✨ O que já está pronto

- **Motor OLAP/MDX** fiel ao Mondrian: parser MDX, avaliador → *CellSet*, geração
  de SQL sobre *star schema*. MDX coberto: `Members`/`Children`, `CrossJoin`,
  tuplas, `Order`/`TopCount`/`BottomCount`/`Filter`, `Union`/`Except`/`Intersect`/
  `Head`/`Tail`/`Distinct`/`Hierarchize`, **membros calculados** (`WITH MEMBER`),
  **named sets** (`WITH SET`), **ranges** (`m1 : m2`), `Sum/Avg/Count/Min/Max/
  Aggregate` sobre conjuntos (% do total), **inteligência de tempo**
  (`PrevMember`/`NextMember`/`Lag`/`Lead`/`YTD`), **múltiplas hierarquias**,
  **parent-child** (lista por nome), `NON EMPTY`, formatação de medidas.
- **API REST** compatível com Saiku: descoberta, query JSON, MDX (parse/execute),
  drill-through + totais, **AI Query API**, cache.
- **5 dialetos SQL** na geração: PostgreSQL (runtime), MySQL/MariaDB, DuckDB,
  SQL Server, Oracle (testados por *golden tests*).
- **UI visual** (`/ui/`): construtor drag-and-drop, cross-tab, gráficos,
  drill-through, editor MDX, CSV, ordenação/paginação, salvar/abrir.
- **Autenticação + papéis** (admin/user), registro aberto, sessão por cookie HMAC.
- **Gerenciador de cubos**: criar/registrar/remover cubos em runtime.

**Ainda não** (erro claro): mostrar todos os membros em `CrossJoin` multi-binding,
*snowflake* (`<Join>`), navegação de árvore/rollup recursivo em parent-child, e
execução de runtime fora do PostgreSQL (os outros dialetos só geram a SQL).

Detalhes completos de cada módulo, da API e do MDX estão no **manual**:
[`DOCS/index.html`](DOCS/index.html) (🇧🇷 PT-BR · 🇬🇧 EN · 🇪🇸 ES).

---

## 🛠️ Desenvolvimento

```sh
make tidy     # go mod tidy (gera go.sum) — em container golang
make vet      # go vet
make test     # go test ./...
make build    # compila tudo
make up       # sobe a stack (engine + postgres:16)
make logs     # segue os logs · make ps (status) · make down (derruba)
```

Tudo roda em containers — não é preciso ter Go instalado no host.

## 🗂️ Layout

```
cmd/cubodw/              CLI (cobra): serve-engine | healthcheck | version
internal/
  config/                configuração via env
  auth/                  store de usuários (bcrypt) + tokens de sessão (HMAC)
  demo/                  schema FoodMart embutido (cubo padrão)
  engine/
    metadata/            IR multidimensional
    schema/mondrian/     import Mondrian XML v3 -> IR
    schema/yaml/         autoria YAML -> IR
    query/               spec de query + Result tipado + formatação
    sql/                 geração de SQL + 5 dialetos (dialect.go)
    mdx/                 lexer + AST + parser MDX
  service/
    discover/            descoberta de metadados (multi-schema)
    queryexec/           execução no Postgres + cache + drill-through
    mdxeval/             avaliador MDX -> CellSet (set ops, WITH, tempo, …)
  web/                   servidor HTTP, auth, schemas, API REST + UI embutida (ui/)
deploy/
  engine/Dockerfile      build multi-stage (distroless)
  docker-compose.yml     engine + postgres:16
  postgres/initdb/       seed do FoodMart
DOCS/                    manual técnico (PT-BR / EN / ES)
```

## 📜 Licença

Código sob **Apache License 2.0** — veja [`LICENSE`](LICENSE). Você pode usar,
modificar e comercializar, **mantendo o crédito ao autor e ao projeto** (avisos de
copyright + [`NOTICE`](NOTICE)).

Os dados/schema de demonstração **FoodMart** vêm do Mondrian e permanecem sob
**Eclipse Public License v1.0 (EPL-1.0)** — não cobertos pela Apache-2.0. Detalhes
em [`THIRD-PARTY-NOTICES.md`](THIRD-PARTY-NOTICES.md).
