# Contributing

Source-available under [PolyForm Noncommercial 1.0.0](LICENSE). Other licenses can be negotiated with the copyright holder.

## Contributions

This project does not accept outside code contributions at the moment, so that its licensing stays in one hand. Issues, bug reports and ideas are very welcome: please [open an issue](https://github.com/pihme/hermetarium/issues/new/choose). Pull requests from outside contributors will be closed without merging.

## Good issues

Pick the matching [issue form](https://github.com/pihme/hermetarium/issues/new/choose) (bug report, security / isolation, documentation, feature request, question). A good report names the Hermetarium version or commit, the wall (weak or strong), host OS and architecture, the habitat you ran, the command, what you expected and what happened, with the relevant part of the I/O log if it helps.

**Security:** the tracker is public. Do not post working exploits, real keys or anything that endangers running habitats. For a serious vulnerability, open an issue that only names the affected area and ask for a private channel.

## Build and test locally

Needs Docker and Go 1.24+. Strong-wall tests also need `/dev/kvm` and **x86_64**.

```bash
make test
```

The default suite (hello-world, echo, agentd against a mock vendor, official-CLI smoke) needs no vendor account. Optional live suite with real vendor keys:

```bash
export HERMETARIUM_ANTHROPIC_API_KEY=...
export HERMETARIUM_XAI_API_KEY=...
export HERMETARIUM_DEEPSEEK_API_KEY=...
make test-live
```

Tests use this tree (`examples/`, `inhabitants/`). From another directory: `export HERMETARIUM_ROOT=/path/to/hermetarium`.
