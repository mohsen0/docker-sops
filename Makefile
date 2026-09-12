BINARY      := docker-sops
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X github.com/mohsen0/docker-sops/internal/version.Version=$(VERSION)
PLUGIN_DIR  ?= $(HOME)/.docker/cli-plugins
SOPS        ?= sops
AGE_KEY     := testdata/age-test-key.txt
AGE_PUBKEY  := $(shell grep '^\# public key:' $(AGE_KEY) 2>/dev/null | cut -d' ' -f4)

.PHONY: build install uninstall test lint e2e fixtures licenses clean release-snapshot release-check

build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/docker-sops

install: build
	mkdir -p $(PLUGIN_DIR)
	install -m 0755 bin/$(BINARY) $(PLUGIN_DIR)/$(BINARY)

uninstall:
	rm -f $(PLUGIN_DIR)/$(BINARY)

test:
	SOPS_AGE_KEY_FILE=$(CURDIR)/$(AGE_KEY) go test -race -cover ./...

lint:
	golangci-lint run ./...

e2e: install
	SOPS_AGE_KEY_FILE=$(CURDIR)/$(AGE_KEY) go test -tags e2e -count=1 ./e2e/...

# Re-encrypt every plaintext fixture to the committed test key. Needs the sops binary.
fixtures:
	@test -n "$(AGE_PUBKEY)" || (echo "no public key in $(AGE_KEY)"; exit 1)
	@for f in testdata/plain/*; do \
		name=$$(basename $$f); ext=$${name##*.}; \
		case $$ext in yaml|yml) t=yaml;; json) t=json;; env) t=dotenv;; ini) t=ini;; *) t=binary;; esac; \
		echo "encrypting $$name ($$t)"; \
		$(SOPS) --encrypt --age $(AGE_PUBKEY) --input-type $$t --output-type $$t $$f > testdata/enc/$$name; \
	done

licenses:
	go run github.com/google/go-licenses@latest report ./cmd/docker-sops --template scripts/licenses.tpl > THIRD_PARTY_LICENSES

release-snapshot:
	goreleaser release --snapshot --clean

release-check:
	goreleaser check

clean:
	rm -rf bin dist coverage.out
