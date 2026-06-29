#!/usr/bin/env bash
# Converte o dump H2 do FoodMart (saiku .../seed/foodmart_h2.sql) para Postgres.
#
# Uso:   convert_h2_to_pg.sh < foodmart_h2.sql > foodmart_pg.sql
#
# Diferenças tratadas (somente na DDL — os literais de dados DATE '...' /
# TIMESTAMP '...' já são válidos no Postgres e ficam intactos):
#   - remove a linha "CREATE USER ... ADMIN;" (específica do H2)
#   - "CREATE CACHED TABLE" -> "CREATE TABLE"
#   - tipos de coluna:  DATETIME -> TIMESTAMP,  TINYINT -> SMALLINT,
#                       DOUBLE -> DOUBLE PRECISION  (só em linhas de definição
#                       de coluna: ^espaços "ident" TIPO ... — nunca em INSERT)
#   - remove o hint "SELECTIVITY <n>" do H2 nas definições de coluna
#   - nomes de índice/constraint não podem ser qualificados por schema:
#       CREATE INDEX PUBLIC."x"   -> CREATE INDEX "x"
#       ADD CONSTRAINT PUBLIC."x" -> ADD CONSTRAINT "x"
#
# O schema "PUBLIC" das tabelas (PUBLIC."tabela") é dobrado para "public" pelo
# Postgres automaticamente, então é mantido.
set -euo pipefail

sed -E \
  -e '/^CREATE USER /d' \
  -e 's/^CREATE CACHED TABLE /CREATE TABLE /' \
  -e 's/^([[:space:]]*"[^"]+"[[:space:]]+)DATETIME/\1TIMESTAMP/' \
  -e 's/^([[:space:]]*"[^"]+"[[:space:]]+)TINYINT/\1SMALLINT/' \
  -e 's/^([[:space:]]*"[^"]+"[[:space:]]+)DOUBLE([ ,]|$)/\1DOUBLE PRECISION\2/' \
  -e 's/ SELECTIVITY [0-9]+//g' \
  -e 's/CREATE INDEX PUBLIC\./CREATE INDEX /' \
  -e 's/ADD CONSTRAINT PUBLIC\./ADD CONSTRAINT /'
