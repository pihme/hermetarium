FROM alpine:3.24
RUN apk add --no-cache curl iproute2 iptables
COPY agentd /usr/local/bin/agentd
ENV AGENTD_VENDOR=anthropic
ENV AGENTD_BASE_URL=http://claude.hermetarium.test
ENV AGENTD_API_KEY=placeholder
EXPOSE 8080
CMD ["/usr/local/bin/agentd"]
