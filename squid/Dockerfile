FROM alpine:3.20
RUN apk add --no-cache squid iptables iproute2 busybox busybox-extras
COPY probe /probe
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
