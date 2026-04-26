set shell := ["bash", "-euo", "pipefail", "-c"]

default:
	@just --list

fmt:
	gofumpt -w tools/ari-cli

fmt-check:
	@test -z "$(gofumpt -l tools/ari-cli)"

nix-fmt-check:
	nix run nixpkgs#nixpkgs-fmt -- --check .

flake-check:
	nix flake check --no-build --keep-going

lint:
	cd tools/ari-cli && golangci-lint run --config ../../.golangci.yml ./...

build:
	cd tools/ari-cli && go build ./...

test:
	cd tools/ari-cli && go test ./...

verify: nix-fmt-check fmt-check lint build test flake-check
	@echo "All checks passed"

ci: verify
	@echo "CI gate complete"

# No-credit binary smoke checks for packaged agent CLIs. These verify that the
# commands Ari targets are installable and can print metadata without auth.
agent-smoke:
	nix run github:numtide/llm-agents.nix#codex -- --version
	nix run github:numtide/llm-agents.nix#claude-code -- --version
	nix run github:numtide/llm-agents.nix#opencode -- --version
