# Seed do FoodMart (Postgres)

Os arquivos deste diretório são montados em `/docker-entrypoint-initdb.d` do
container `postgres:16` e executados **uma única vez**, na primeira inicialização
do volume de dados (volume `cubodw_pgdata`). Para re-executar, derrube o volume:
`docker compose -f deploy/docker-compose.yml down -v`.

## Arquivos

- `10-foodmart.sql.gz` — dump do FoodMart já convertido para Postgres
  (37 tabelas; `sales_fact_1997` ≈ 86.837 linhas). O entrypoint do Postgres
  descompacta `*.sql.gz` automaticamente.

## Como foi gerado / como regenerar

A origem é o dump **H2** do FoodMart embutido no Saiku
(`saiku-4.5.2/saiku-launcher/src/main/resources/seed/foodmart_h2.sql.zip`),
convertido pelo script `deploy/postgres/convert_h2_to_pg.sh`:

```sh
# 1) extrair o dump H2 de dentro do zip do Saiku
unzip -p saiku-4.5.2.zip \
  saiku-4.5.2/saiku-launcher/src/main/resources/seed/foodmart_h2.sql.zip > /tmp/h2.zip
unzip -p /tmp/h2.zip foodmart_h2.sql > /tmp/foodmart_h2.sql

# 2) converter H2 -> Postgres e compactar para o initdb
bash deploy/postgres/convert_h2_to_pg.sh < /tmp/foodmart_h2.sql \
  | gzip -c > deploy/postgres/initdb/10-foodmart.sql.gz
```

Validação rápida (com a stack no ar):

```sh
docker exec -i cubodw-postgres-1 psql -U cubodw -d foodmart \
  -c 'SELECT count(*) FROM public."sales_fact_1997";'   # ~86837
```
