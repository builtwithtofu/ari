# Ari TODO

## Architecture Plan

See `.sisyphus/plans/ari-v0-architecture.md` for the full v0 architecture plan.

## Implementation Phases (from plan)

### Phase 1: Foundation
- [ ] Define Go module structure and packages
- [ ] Implement protocol types (events, messages)
- [ ] Implement world manager (SQLite + JSON mirror)
- [ ] Implement VCS detector
- [ ] Add basic tests for each component

### Phase 2: Command Surface
- [ ] Implement `ari init` (create world, detect VCS, emit events)
- [ ] Implement `ari ask` (query world without LLM)
- [ ] Add command tests

### Phase 3: Agent Loop
- [ ] Define provider interface
- [ ] Implement mock provider for testing
- [ ] Implement agent state machine
- [ ] Wire `ari plan` to agent loop
- [ ] Add agent tests

### Phase 4: Full Commands
- [ ] Complete `ari plan` with real LLM integration
- [ ] Implement `ari build`
- [ ] Implement `ari review`
- [ ] Implement `ari ask` with LLM

### Phase 5: Providers & Polish
- [ ] Add OpenAI provider
- [ ] Add Anthropic provider
- [ ] Error handling and edge cases
- [ ] Documentation

## Later (Separate Project)

- [ ] Design channel gateway protocol and adapters in a separate repository.