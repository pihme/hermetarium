FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
	ca-certificates curl bash iproute2 iptables procps \
	&& rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://claude.ai/install.sh | bash \
	&& ln -sf /root/.local/bin/claude /usr/local/bin/claude
ENV PATH="/root/.local/bin:/usr/local/bin:${PATH}"
WORKDIR /work
COPY porter /usr/local/bin/porter
COPY settings.json /root/.claude/settings.json
ENV HARNESS=claude
ENV HARNESS_WORKDIR=/work
ENV ANTHROPIC_BASE_URL=http://claude.hermetarium.test
ENV IS_SANDBOX=1
ENV CLAUDE_CODE_BUBBLEWRAP=1
ENV CI=true
# Dummy so the CLI boots; Squid replaces x-api-key on the logged path.
ENV ANTHROPIC_API_KEY=not-the-supervisor-key
EXPOSE 8080
CMD ["/usr/local/bin/porter"]
