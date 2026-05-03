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

# No-credit binary smoke checks for agent CLIs. Override command paths with
# ARI_CODEX_EXECUTABLE, ARI_CLAUDE_EXECUTABLE, or ARI_OPENCODE_EXECUTABLE.
agent-smoke:
	@bash -euo pipefail -c 'for spec in "codex:${ARI_CODEX_EXECUTABLE:-codex}" "claude:${ARI_CLAUDE_EXECUTABLE:-claude}" "opencode:${ARI_OPENCODE_EXECUTABLE:-opencode}"; do harness="${spec%%:*}"; command="${spec#*:}"; if ! command -v "${command}" >/dev/null 2>&1; then env_name=$(printf "%s" "${harness}" | tr "[:lower:]" "[:upper:]"); printf "Ari agent smoke: missing %s executable %q. Install it or set ARI_%s_EXECUTABLE to an absolute path. No auth or model call was attempted.\n" "${harness}" "${command}" "${env_name}" >&2; exit 127; fi; "${command}" --version; done'
