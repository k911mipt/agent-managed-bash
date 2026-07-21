.PHONY: doctor generated-model-compile protocol-schema-test schema-check schema-generate schema-generated-check state-schema-test

GO_GENERATED ?= internal/protocol/generated/models.gen.go
TS_GENERATED ?= plugins/opencode/src/generated/protocol.gen.ts
SCHEMA_ROOT ?= schemas/v1
GO_GENERATOR ?= go tool go-jsonschema

doctor:
	@./scripts/doctor.sh

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
	@bun run --silent --cwd plugins/opencode typecheck

protocol-schema-test:
	@GOTOOLCHAIN=local go test -run 'Test_(ProtocolSchemaFixtures|FixtureManifest)' ./internal/protocol
	@bun test plugins/opencode/src/protocol-schema.test.ts

state-schema-test:
	@GOTOOLCHAIN=local go test ./internal/state

schema-check: schema-generated-check protocol-schema-test generated-model-compile state-schema-test
