GO ?= go
RUN ?= $(shell command -v agent-test 2>/dev/null)
CONFIGS := hyrule.yaml examples/inlinego/hyrule.yaml examples/localplans/hyrule.yaml examples/transitions/hyrule.yaml examples/unions/hyrule.yaml testdata/decisionbench/hyrule.yaml
.PHONY: build generate generated-check test vet format-check check-source
build: generate format-check check-source
	$(RUN) $(GO) build ./...
generate:
	@set -e; for config in $(CONFIGS); do $(RUN) $(GO) run ./cmd/hyrule generate --config "$$config"; done
	cp -f testdata/decisionbench/legacy_test.go.txt testdata/decisionbench/legacy_test.go
generated-check:
	@set -e; for config in $(CONFIGS); do $(RUN) $(GO) run ./cmd/hyrule generate --check --config "$$config"; done
	cmp testdata/decisionbench/legacy_test.go.txt testdata/decisionbench/legacy_test.go
test: generate
	$(RUN) $(GO) test -p=1 -parallel=4 ./...
vet: generate
	$(RUN) $(GO) vet ./...

format-check:
	@test -z "$$(gofmt -l $$(git ls-files '*.go'))"

check-source:
	python3 scripts/check-source.py
