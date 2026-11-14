.PHONY: deps run build test test-unit test-integration test-federation test-pg-up test-pg-down lint migrate-up migrate-down migrate-create ui docker

ifneq (,$(wildcard .env))
include .env
export
endif

TEST_PG_CONTAINER ?= directory-test-postgres
TEST_PG_PORT ?= 55433
TEST_DATABASE_URL ?= postgres://directory:dirtest@127.0.0.1:$(TEST_PG_PORT)/directory?sslmode=disable

deps:
	go mod download

run:
	go run ./cmd serve

build:
	CGO_ENABLED=0 go build -trimpath -o bin/directory ./cmd

# Builds the web UI into ui/build (embedded by `go build`).
ui:
	cd ui && npm ci --no-audit --no-fund && npm run build
	touch ui/build/.gitkeep

test: test-unit

# Unit tests; integration tests skip unless TEST_DATABASE_URL is set.
test-unit:
	go test -race ./...

test-pg-up:
	docker run -d --rm --name $(TEST_PG_CONTAINER) -e POSTGRES_USER=directory -e POSTGRES_PASSWORD=dirtest \
		-p 127.0.0.1:$(TEST_PG_PORT):5432 postgres:18-alpine
	@until docker exec $(TEST_PG_CONTAINER) pg_isready -U directory -q; do sleep 1; done; sleep 1

test-pg-down:
	docker stop $(TEST_PG_CONTAINER)

test-integration:
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -race -count=1 ./...

# Two directories + Postgres + fake DNS/auth in docker (test/federation):
# import, local DNS re-verification, conflicts, blocks, deletes, the
# built-in main peer and its persistent disable. Leaves nothing behind.
# dir-c runs in stack config file mode (test/federation/config-c, mounted
# read-only).
FED_COMPOSE = docker compose -f test/federation/compose.yml
FED_CONFIG_C = test/federation/config-c
test-federation:
	rm -rf $(FED_CONFIG_C) && mkdir -p $(FED_CONFIG_C) && chmod 755 $(FED_CONFIG_C) && \
		cp test/federation/boson.dir-c.yaml $(FED_CONFIG_C)/boson.yaml && chmod 644 $(FED_CONFIG_C)/boson.yaml
	$(FED_COMPOSE) up -d --build --wait postgres fakes dir-a dir-b dir-c || ($(FED_COMPOSE) logs; $(FED_COMPOSE) down -v; rm -rf $(FED_CONFIG_C); exit 1)
	(cd test/federation && go test -tags federation_e2e -count=1 -v -timeout 10m .); \
		status=$$?; [ $$status -eq 0 ] || $(FED_COMPOSE) logs --tail 200 dir-a dir-b dir-c; \
		$(FED_COMPOSE) down -v --remove-orphans; rm -rf $(FED_CONFIG_C); exit $$status

lint:
	go vet ./...
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

migrate-create:
	migrate create -ext sql -dir db/migrations -seq $(name)

migrate-up:
	go run ./cmd migrate up

migrate-down:
	go run ./cmd migrate down

docker:
	docker build -t boson-directory:dev .
