# Ari

> Every project is a world. Ari helps you navigate it.

---

Ideas are hard to hold. A codebase grows and the original intent fades. A new project starts and the gap between what you imagine and what you can build feels vast. A team expands and the shared understanding that once lived in one person's head becomes diffuse, fragile, implicit.

Ari exists because ideas deserve better than that.

Current repository status: Ari CLI is in an ultra-reset baseline. The `ari` command is intentionally
minimal while the v0 loop contract is defined.

At its core, Ari is a runtime for navigating the world of your ideas. Not just a coding tool. Not just an agent harness. A system for making the invisible visible - the architecture behind your code, the decisions behind your architecture, the intent behind your decisions.

Every project you touch with Ari becomes a **world**. A living, structured representation of what exists, what was decided, what is planned, and what remains unknown. The world grows as you work. It persists between sessions. It can be handed to a new collaborator, picked up after months away, or interrogated when you've forgotten why something was built the way it was.

**Ari** is your guide through that world.

On a greenfield project, Ari helps you build the world from the first question - exploring the shape of the idea before a single line is written, surfacing gaps, establishing the map. On an existing codebase, Ari discovers the world that's already there - reading the terrain, learning the conventions, making sense of what accumulated over time.

In both cases, Ari asks before she acts. She plans before she builds. She surfaces what she doesn't know rather than guessing. The result is a guide you can actually trust, and a world you can actually navigate.

---

## How it works

```bash
ari init          # start a new world, or discover an existing one
ari plan          # explore the idea space before committing to a path
ari build         # execute against a validated plan
ari review        # understand what changed and why
ari ask           # interrogate the world - what exists, what was decided
```

Under the hood, Ari is a headless runtime and protocol. Ari is the presence you interact with. The world is the persistent artifact she builds and maintains for your project.

The CLI is purely responsible for the agent loop, human-in-the-loop protocol, and structured output. Everything else - visualization, rendering, UI - is a client concern. Ari works headlessly in CI, in Docker, over SSH, piped between processes, driven by other agents. It doesn't care how you consume it.

---

## The north star

Most tools optimize for *output* - more code, faster, with less friction.

Ari optimizes for *understanding*.

The bet is that the bottleneck in building things isn't writing code. It's navigating the world of ideas well enough to write the right code, in the right place, for the right reasons. Ari exists to close that gap - not by automating away the thinking, but by giving you a guide who can hold the map while you explore the terrain.

A world well understood is a world well built.
