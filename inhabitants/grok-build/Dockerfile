FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
	ca-certificates curl bash iproute2 iptables procps \
	&& rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://x.ai/cli/install.sh | bash \
	&& ln -sf /root/.grok/bin/grok /usr/local/bin/grok
ENV PATH="/root/.grok/bin:/usr/local/bin:${PATH}"
WORKDIR /work
COPY porter /usr/local/bin/porter
COPY config.toml /root/.grok/config.toml
ENV HARNESS=grok
ENV HARNESS_WORKDIR=/work
# Grok 1.0.77 keeps the sampler on https://api.x.ai/v1 unless the second
# variable is set. The first one only moves feedback and trace calls.
# The gate credential must be an xAI API key. A login session token gets
# 401 from GET /v1/api-key, and Grok stops.
ENV GROK_CLI_CHAT_PROXY_BASE_URL=http://grok.hermetarium.test
ENV GROK_XAI_API_BASE_URL=http://grok.hermetarium.test/v1
ENV GROK_DISABLE_AUTOUPDATER=1
ENV GROK_SANDBOX=off
# Dummy so the CLI boots; Squid replaces Authorization on the logged path.
ENV XAI_API_KEY=not-the-supervisor-key
EXPOSE 8080
CMD ["/usr/local/bin/porter"]
