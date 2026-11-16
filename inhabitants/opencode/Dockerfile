FROM debian:bookworm-slim
# sshd and tmux are the strong-wall terminal. build-essential is the compiler the inhabitant uses.
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
	ca-certificates curl bash iproute2 iptables procps git ripgrep \
	tmux openssh-server build-essential \
	&& rm -rf /var/lib/apt/lists/*
# 1.19.5 completed a turn through the gate. The hash is that GitHub release asset.
# npm's postinstall downloads the same file, and a later opencode would fetch models.
RUN curl -fsSL -o /tmp/opencode.tar.gz \
	https://github.com/anomalyco/opencode/releases/download/v1.19.5/opencode-linux-x64.tar.gz \
	&& echo "588310a336fa3b9df36b3f0225b26f679d453efaad0cf3d66c3651ef3ec1647a  /tmp/opencode.tar.gz" | sha256sum -c - \
	&& tar -xzf /tmp/opencode.tar.gz -C /usr/local/bin opencode \
	&& chmod 755 /usr/local/bin/opencode \
	&& rm /tmp/opencode.tar.gz \
	&& OPENCODE_DISABLE_AUTOUPDATE=1 OPENCODE_DISABLE_MODELS_FETCH=1 opencode --version
WORKDIR /work
COPY porter /usr/local/bin/porter
COPY opencode.json /root/.config/opencode/opencode.json
ENV HARNESS=opencode
ENV HARNESS_WORKDIR=/work
# A first start fetches models and an update unless both are off. rg is installed so it does not fetch that either.
ENV OPENCODE_DISABLE_AUTOUPDATE=1
ENV OPENCODE_DISABLE_MODELS_FETCH=1
# Dummy so the CLI boots; Squid replaces Authorization on the logged path.
ENV OPENCODE_API_KEY=not-the-supervisor-key
EXPOSE 8080
CMD ["/usr/local/bin/porter"]
