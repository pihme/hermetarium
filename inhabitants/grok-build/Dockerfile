FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
	ca-certificates curl bash iproute2 iptables procps \
	&& rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://x.ai/cli/install.sh | bash \
	&& ln -sf /root/.grok/bin/grok /usr/local/bin/grok
ENV PATH="/root/.grok/bin:/usr/local/bin:${PATH}"
WORKDIR /work
COPY porter /usr/local/bin/porter
ENV HARNESS=grok
ENV HARNESS_WORKDIR=/work
ENV GROK_CLI_CHAT_PROXY_BASE_URL=http://grok.hermetarium.test
# Dummy so the CLI boots; Squid replaces Authorization on the logged path.
ENV XAI_API_KEY=not-the-supervisor-key
EXPOSE 8080
CMD ["/usr/local/bin/porter"]
