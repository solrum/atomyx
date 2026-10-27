# Atomyx: user documentation

## Purpose

Index of the end-user documentation for Atomyx: installing the core
runtime, driving a device through MCP, and writing YAML scripts.
Read this file to find the right doc, not to learn the product
itself.

Contributor and implementation documentation (architecture, internal
protocols, pitfalls) lives in [`.claude/docs/`](../.claude/docs/)
instead. The split exists so an end user opening this directory sees
only material about using Atomyx, while a contributor works from the
deeper context under `.claude/`.

## Start here

[`getting-started.md`](./getting-started.md) walks from a fresh
clone to an AI agent driving a device, or a YAML script running
against one.

## Reference

| File | Answers |
|---|---|
| [`cli.md`](./cli.md) | What does the `atomyx` command and each of its flags do? |
| [`yml-script-reference.md`](./yml-script-reference.md) | What does the YAML script format support? |
| [`pointer.md`](./pointer.md) | How do I author a specific touch gesture? |
| [`device-setup.md`](./device-setup.md) | How do I prepare an Android or iOS device to be driven? |
| [`mcp-tools.md`](./mcp-tools.md) | What does each MCP tool do and how does it behave? |
| [`known-limitations.md`](./known-limitations.md) | What does Atomyx currently not do, or do wrongly, that a user can hit, and how do I work around it? |
| [`status.md`](./status.md) | What does each Atomyx module do today, and how do I check it? |
| [`reliability.md`](./reliability.md) | How is reliability measured, what are the thresholds, and how do I reproduce the figures? |
| [`maestro-parity.md`](./maestro-parity.md) | Which Maestro commands does Atomyx support, partially support, or leave out, and why? |

## Personas served by this directory

- **Script author**: writing YAML tests against the `atomyx` CLI or
  in CI.
- **Agent operator**: wiring the `atomyx-mcp` server into an AI
  client and driving a device conversationally.
- **Device maintainer**: installing and troubleshooting the Android
  and iOS agents.

A separate desktop IDE, Atomyx Studio, is under development and is
not covered by this directory.

See [`../.claude/docs/architecture.md`](../.claude/docs/architecture.md)
for the system's design contract, and
[`../.claude/docs/`](../.claude/docs/) generally for the contributor
view.
