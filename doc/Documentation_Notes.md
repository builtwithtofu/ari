# Documentation Notes

This file tracks user-facing documentation we want to write after implementation stabilizes.

## Documentation Taxonomy

- Project-facing specification and product docs belong in `doc/`.
- GAIA operational plans and runtime artifacts belong in `.gaia/`.
- Canonical north star/specification: `doc/SPECIFICATION.md`.

## Product Overview

- What Project GAIA is and the current pre-alpha status
- GAIA mode goals: human-in-the-loop and agentic workflows
- Difference between `lean`, `full`, and `custom` operation profiles

## Installation and Setup

- Nix + Bun setup (`nix develop`, `bun install`)
- Plugin package layout and portability boundary
- First-run bootstrap with `ari flow start`

## Configuration Guide

- `.gaia/config.jsonc` structure and precedence with global config
- Model overrides, fallback behavior, and safe defaults
- Operation profile settings:
  - `lean`: GAIA + ATHENA + HEPHAESTUS + DEMETER
  - `full`: all agents enabled
  - `custom`: subsystem mix (GAIA always included)

## Operating Modes

- `supervised`, `autopilot`, and `locked` behavior
- Checkpoint expectations and review depth settings
- Safety controls and dangerous operation handling

## Agent Reference

- GAIA: orchestration role and boundaries
- ATHENA: recon and routing role
- HEPHAESTUS: implementation role
- DEMETER: memory and decision capture role
- JSON contract expectations for delegated outputs

## Tools and Hooks

- `delegate_gaia`, `collect_results`, and `plan_gaia` usage
- Hook behavior for decision capture and rejection feedback
- JSON parse retry behavior and fallback metadata

## Work Artifact Conventions

- `.gaia/{work-unit}/plan.md`, `log.md`, `decisions.md` lifecycle
- How wave IDs and work units are named
- How to review and curate recorded decisions

## Troubleshooting

- Common config and schema errors
- Missing model and fallback resolution behavior
- Invalid JSON delegation responses and recovery steps

## Workflow Examples

- Lean mode task walkthrough
- Custom subsystem profile walkthrough
- Transition path from lean to full profile
