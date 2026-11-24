FROM debian:bookworm-slim
# sshd and tmux are the strong-wall terminal. build-essential is the compiler the inhabitant uses.
# docker.io is the engine inside the guest. dockerd leaves iptables alone.
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
	ca-certificates curl bash iproute2 iptables procps git ripgrep \
	tmux openssh-server build-essential docker.io \
	&& rm -rf /var/lib/apt/lists/*
# Current OpenCode 2, not a pinned release. The binary updates itself.
# The public model list stays off for this check.
RUN curl -fsSL https://opencode.ai/v2/install | bash -s -- --no-modify-path \
	&& ln -sf /root/.opencode/bin/opencode /usr/local/bin/opencode \
	&& OPENCODE_DISABLE_MODELS_FETCH=1 opencode --version
# gh 2.104.0. The hash is that GitHub release asset.
RUN curl -fsSL -o /tmp/gh.tar.gz \
	https://github.com/cli/cli/releases/download/v2.104.0/gh_2.104.0_linux_amd64.tar.gz \
	&& echo "98cb101ad87dbbd3f6b2f2bfcbd43197ccd07643140121d8a9cfaa91eadbc24e  /tmp/gh.tar.gz" | sha256sum -c - \
	&& tar -xzf /tmp/gh.tar.gz -C /tmp \
	&& mv /tmp/gh_2.104.0_linux_amd64/bin/gh /usr/local/bin/gh \
	&& chmod 755 /usr/local/bin/gh \
	&& rm -rf /tmp/gh.tar.gz /tmp/gh_2.104.0_linux_amd64 \
	&& gh --version
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
	nano bash-completion \
	&& rm -rf /var/lib/apt/lists/*
WORKDIR /work
COPY porter /usr/local/bin/porter
COPY opencode.json /root/.config/opencode/opencode.json
COPY auto-models.ts /root/.config/opencode/plugins/auto-models.ts
COPY bashrc /root/.bashrc
COPY bash_profile /root/.bash_profile
COPY daemon.json /etc/docker/daemon.json
# The same static guide in every inhabitant image. Nothing about the habitat is in it.
COPY day-one.md /etc/hermetarium/day-one.md
# CA trust vars for login shells, matching day-one.md. Copied from the context for a hand build.
COPY profile.d-ca.sh /etc/profile.d/hermetarium-ca.sh
COPY start.sh /usr/local/bin/opencode-start
RUN chmod 755 /usr/local/bin/opencode-start /usr/local/bin/porter
ENV HARNESS=opencode
ENV HARNESS_WORKDIR=/work
# models.opencode.ai stays off. rg is installed so OpenCode does not fetch that either.
# Autoupdate stays on. opencode.json is what turns it on.
ENV PATH="/root/.opencode/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
ENV OPENCODE_DISABLE_MODELS_FETCH=1
# Dummy so the CLI boots; Squid replaces Authorization on the logged path.
ENV OPENCODE_API_KEY=not-the-supervisor-key
EXPOSE 8080
CMD ["/usr/local/bin/opencode-start"]
