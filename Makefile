# CuboDW — atalhos de build. Go roda sempre em container (Docker-first).

GOIMG ?= golang:1.26
COMPOSE = docker compose -f deploy/docker-compose.yml

# Go dentro de container, com caches persistentes em volumes nomeados.
DOCKER_GO = docker run --rm \
	-v $(CURDIR):/src -w /src \
	-v cubodw-gomod:/go/pkg/mod \
	-v cubodw-gobuild:/root/.cache/go-build \
	$(GOIMG)

.PHONY: tidy vet test build up down logs ps image clean

tidy:        ## go mod tidy (gera go.sum)
	$(DOCKER_GO) go mod tidy

vet:         ## go vet
	$(DOCKER_GO) go vet ./...

test:        ## go test
	$(DOCKER_GO) go test ./...

build:       ## compila tudo (sem gerar binário)
	$(DOCKER_GO) go build ./...

up:          ## sobe a stack (engine + postgres:16)
	$(COMPOSE) up --build -d

down:        ## derruba a stack
	$(COMPOSE) down

logs:        ## segue os logs
	$(COMPOSE) logs -f

ps:          ## status dos serviços
	$(COMPOSE) ps

image:       ## builda a imagem do engine
	docker build -f deploy/engine/Dockerfile -t cubodw-engine:dev .

clean:       ## remove volumes de cache do Go
	-docker volume rm cubodw-gomod cubodw-gobuild
