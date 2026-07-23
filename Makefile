.PHONY: cli-acceptance cli-build cli-race-test cli-test doctor e2e-test generated-model-compile go-check installed-opencode-e2e opencode-plugin-config-test plugin-bundle plugin-bundle-test plugin-test plugin-typecheck protocol-schema-test release-package release-package-test release-version-test runner-test schema-check schema-generate schema-generated-check state-schema-test verify

CLI_BINARY ?= bin/managed-bash
CLI_PACKAGE ?= ./cmd/managed-bash
override RELEASE_VERSION := $(shell tr -d '\r\n' < VERSION)
CLI_LDFLAGS ?= -X main.binaryVersion=$(RELEASE_VERSION)
DIST_DIR ?= dist
GO_GENERATED ?= internal/protocol/generated/models.gen.go
TS_GENERATED ?= plugins/opencode/src/generated/protocol.gen.ts
SCHEMA_ROOT ?= schemas/v1
GO_GENERATOR ?= go tool go-jsonschema

doctor:
	@./scripts/doctor.sh

cli-test:
	@GOTOOLCHAIN=local go test ./internal/protocol ./internal/cli $(CLI_PACKAGE)

cli-race-test:
	@GOTOOLCHAIN=local go test -race -shuffle=on -count=1 ./internal/protocol ./internal/cli $(CLI_PACKAGE)

cli-build:
	@mkdir -p "$(dir $(CLI_BINARY))"
	@GOTOOLCHAIN=local go build -trimpath -buildvcs=false -ldflags "$(CLI_LDFLAGS)" -o "$(CLI_BINARY)" $(CLI_PACKAGE)

cli-acceptance: cli-build
	@sh tests/cli_binary_test.sh "$(abspath $(CLI_BINARY))"

release-version-test:
	@sh tests/release_version_test.sh

schema-generate:
	@set -eu; stage=$$(mktemp -d); trap 'rm -rf "$$stage"' EXIT HUP INT TERM; \
		GOTOOLCHAIN=local $(GO_GENERATOR) \
			--only-models \
			--package generated \
			--struct-name-from-title \
			--capitalization ID \
			--tags json \
			--output "$$stage/models.gen.go" \
			"$(SCHEMA_ROOT)/models.schema.json" \
			"$(SCHEMA_ROOT)/request.schema.json" \
			"$(SCHEMA_ROOT)/response.schema.json" \
			"$(SCHEMA_ROOT)/state.schema.json"; \
		bun run --silent scripts/generate-schema-models.ts "$$stage/protocol.gen.ts" "$(SCHEMA_ROOT)"; \
		mkdir -p "$(dir $(GO_GENERATED))" "$(dir $(TS_GENERATED))"; \
		mv "$$stage/models.gen.go" "$(GO_GENERATED)"; \
		mv "$$stage/protocol.gen.ts" "$(TS_GENERATED)"

schema-generated-check:
	@./scripts/schema_generated_check.sh

generated-model-compile:
	@GOTOOLCHAIN=local go test ./internal/protocol/...
	@bun run --silent typecheck:scripts
	@$(MAKE) --no-print-directory plugin-typecheck

plugin-typecheck:
	@bun run --silent --cwd plugins/opencode typecheck

plugin-bundle:
	@rm -rf plugins/opencode/dist
	@mkdir -p plugins/opencode/dist
	@bun build plugins/opencode/src/index.ts \
		--target=bun \
		--format=esm \
		--packages=bundle \
		--outfile=plugins/opencode/dist/managed-bash.js \
		--define __MANAGED_BASH_RELEASE_VERSION__='"$(RELEASE_VERSION)"'

plugin-bundle-test:
	@sh tests/plugin_bundle_test.sh

opencode-plugin-config-test: plugin-bundle
	@sh tests/opencode_plugin_config_test.sh

release-package: plugin-bundle
	@test -n "$(SOURCE_DATE_EPOCH)" || { printf '%s\n' 'SOURCE_DATE_EPOCH is required' >&2; exit 1; }
	@test -n "$(DIST_DIR)" || { printf '%s\n' 'DIST_DIR is required' >&2; exit 1; }
	@mkdir -p "$(DIST_DIR)"
	@rm -f "$(DIST_DIR)"/agent-managed-bash-*.tar.gz
	@GOTOOLCHAIN=local go run ./cmd/release-package build --root "$(CURDIR)" --output "$(abspath $(DIST_DIR))"

release-package-test:
	@GOTOOLCHAIN=local go test -race -shuffle=on -count=1 ./internal/release ./internal/installer ./cmd/release-package ./cmd/managed-bash
	@sh tests/release_package_test.sh

installed-opencode-e2e: release-package
	@sh tests/installed_opencode_test.sh

e2e-test: release-package-test
	@sh tests/installed_opencode_test.sh

verify:
	@test -n "$(SOURCE_DATE_EPOCH)" || { printf '%s\n' 'SOURCE_DATE_EPOCH is required' >&2; exit 1; }
	@$(MAKE) --no-print-directory doctor
	@$(MAKE) --no-print-directory schema-check
	@$(MAKE) --no-print-directory runner-test
	@$(MAKE) --no-print-directory cli-race-test
	@$(MAKE) --no-print-directory cli-acceptance
	@$(MAKE) --no-print-directory release-version-test
	@$(MAKE) --no-print-directory plugin-test
	@$(MAKE) --no-print-directory plugin-typecheck
	@$(MAKE) --no-print-directory plugin-bundle-test
	@$(MAKE) --no-print-directory opencode-plugin-config-test
	@$(MAKE) --no-print-directory release-package-test
	@sh tests/installed_opencode_test.sh
	@$(MAKE) --no-print-directory go-check

plugin-test:
	@bun run --silent --cwd plugins/opencode test

protocol-schema-test:
	@GOTOOLCHAIN=local go test -run 'Test_(ProtocolSchemaFixtures|FixtureManifest)' ./internal/protocol
	@bun test plugins/opencode/src/protocol-schema.test.ts

state-schema-test:
	@GOTOOLCHAIN=local go test ./internal/state

runner-test:
	@GOTOOLCHAIN=local go test -race -shuffle=on -count=1 ./internal/runner

go-check:
	@GOTOOLCHAIN=local go test -race -shuffle=on -count=1 ./...
	@GOTOOLCHAIN=local go vet ./...
	@GOTOOLCHAIN=local go build ./...

schema-check: schema-generated-check protocol-schema-test generated-model-compile state-schema-test
