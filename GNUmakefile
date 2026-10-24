.DEFAULT_GOAL := test
-include GNUmakefile.local

MITOGEN_VERSION = 0.3.51
GO_SRC := go.mod go.sum $(shell rg --files cmd internal)

.PHONY: build build-deploy install assets lint test ci migrate db-migrate run run-engineering run-engineering-cron collect deploy

install:
	go mod download

build build-deploy: dist/bsmiweb

dist/bsmiweb: $(GO_SRC)
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o $@ ./cmd/bsmiweb

# Only needed when updating the checked-in, self-hosted browser Sentry bundle.
assets:
	npm ci
	npm run build

lint:
	@files=$$(gofmt -l cmd internal); test -z "$$files" || { echo "gofmt needed: $$files"; exit 1; }
	go vet ./...

test:
	go test ./...

ci: lint
	go test -race ./...
	go mod tidy
	git diff --exit-code go.mod go.sum
	$(MAKE) build-deploy

migrate db-migrate:
	go run ./cmd/bsmiweb migrate

run run-engineering: db-migrate
	go run ./cmd/bsmiweb serve

run-engineering-cron: db-migrate
	go run ./cmd/bsmiweb cron

collect:
	go run ./cmd/bsmiweb collect $(JOB)

# Mitogen 0.3.49+ is required for ansible-core 2.21 (Ansible 14).
ansible/lib/.mitogen-$(MITOGEN_VERSION):
	rm -rf ansible/lib
	pip install --target=ansible/lib 'mitogen==$(MITOGEN_VERSION)'
	touch $@

deploy: build-deploy ansible/lib/.mitogen-$(MITOGEN_VERSION)
	./scripts/deploy.sh
