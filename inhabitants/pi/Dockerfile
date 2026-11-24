# Pi coding inhabitant, modeled on inhabitants/opencode (the OpenCode TUI image).
# sshd and tmux are the strong-wall terminal. build-essential is the compiler
# the inhabitant uses. docker.io is the engine inside the guest; dockerd leaves
# iptables alone. tini is PID 1 (a subreaper) so finished background processes
# are reaped instead of piling up as <defunct> zombies — that is what lets the
# file-trigger wake and any background tasks report real liveness.
FROM node:22-bookworm-slim
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
	ca-certificates curl bash iproute2 iptables procps git ripgrep \
	tmux openssh-server build-essential docker.io tini \
	&& rm -rf /var/lib/apt/lists/*
# Pi 1.6, not pinned to a patch. The npm install is the documented path and
# needs no lifecycle scripts. Node 22 is the base image.
RUN npm install -g --ignore-scripts @earendil-works/pi-coding-agent @earendil-works/pi-durable \
	&& pi --version
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
# The wake extension. It watches /tmp/agent-trigger.txt and, on a write, calls
# pi.sendMessage({triggerTurn:true}) to inject the event into the live session.
COPY file-trigger.ts /root/.pi/agent/extensions/file-trigger.ts
# Model wiring for the gate. The gate is model.hermetarium.test; Squid replaces
# Authorization on the logged path. The key stays in squid.conf.
COPY models.json /root/.pi/agent/models.json
COPY bashrc /root/.bashrc
COPY bash_profile /root/.bash_profile
COPY daemon.json /etc/docker/daemon.json
COPY start.sh /usr/local/bin/pi-start
RUN chmod 755 /usr/local/bin/pi-start /usr/local/bin/porter
ENV HARNESS=pi
ENV HARNESS_WORKDIR=/work
# Dummy so the CLI boots; Squid replaces Authorization on the logged path.
ENV OPENAI_API_KEY=not-the-supervisor-key
ENV PI_TELEMETRY=0
# tini is PID 1 (a subreaper) so background tasks are reaped.
ENTRYPOINT ["/usr/bin/tini", "--"]
EXPOSE 8080
CMD ["/usr/local/bin/pi-start"]
