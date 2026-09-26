FROM node:26-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
	ca-certificates curl iproute2 iptables procps \
	&& rm -rf /var/lib/apt/lists/*
RUN npm install -g @deepseek-ai/dsh
WORKDIR /work
COPY porter /usr/local/bin/porter
ENV HARNESS=dsh
ENV HARNESS_WORKDIR=/work
ENV OPENAI_BASE_URL=http://deepseek.hermetarium.test/v1
ENV DEEPSEEK_BASE_URL=http://deepseek.hermetarium.test
ENV DSH_HOME=/root/.dsh
# Dummy so the CLI boots; Squid replaces Authorization on the logged path.
ENV DEEPSEEK_API_KEY=not-the-supervisor-key
EXPOSE 8080
CMD ["/usr/local/bin/porter"]
