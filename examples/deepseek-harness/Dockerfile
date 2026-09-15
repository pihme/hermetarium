FROM alpine:3.20
RUN apk add --no-cache curl iproute2 iptables
COPY agentd /usr/local/bin/agentd
ENV AGENTD_VENDOR=openai
ENV AGENTD_BASE_URL=http://deepseek.hermetarium.test
ENV AGENTD_API_KEY=placeholder
EXPOSE 8080
CMD ["/usr/local/bin/agentd"]
