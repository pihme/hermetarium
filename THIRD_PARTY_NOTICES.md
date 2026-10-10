# Third-party software

Hermetarium itself is licensed under [PolyForm Noncommercial 1.0.0](LICENSE). This file lists the third-party software it contains or runs, with the notices their licenses ask for.

## Compiled into the `hermetarium` and `porter` binaries

Both binaries use only the Go standard library (`go.mod` has no requirements). The Firecracker helper scripts in `firecracker-helper/` are this project's own code, embedded in the `hermetarium` binary.

### Go standard library and runtime

BSD 3-Clause license, <https://go.dev/LICENSE>.

```
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```
## External programs and images (run, not included)

Hermetarium doesn't contain these. The supervisor runs them as separate processes or containers (see [Getting started](README.md#getting-started)); their own licenses apply to them.

| Program | License | Used by |
| --- | --- | --- |
| [Docker](https://www.docker.com/) / [runc](https://github.com/opencontainers/runc) | Apache-2.0 | Weak wall; also runs the Squid container and the Firecracker helper container |
| [Squid](https://www.squid-cache.org/), image `ubuntu/squid:6.6-24.04_beta` | GPL-2.0-or-later | The only network path of a habitat; pulled from Docker Hub at `create`. Runs as a sibling process; Hermetarium does not link it |
| [Firecracker](https://github.com/firecracker-microvm/firecracker) v1.16.1 | Apache-2.0 | Strong wall; downloaded from its GitHub release into the cache dir on the first strong `create` or `make test` |

**Images you build.** The Dockerfiles in `inhabitants/` and `examples/` install third-party software (base images `debian:bookworm-slim`, `node:26-bookworm-slim`, `alpine:3.24`, and the vendor CLIs they name), each under its own license. This project doesn't publish those images; if you distribute one, its obligations are yours.
