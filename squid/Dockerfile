FROM alpine:3.20
RUN apk add --no-cache squid iptables iproute2 busybox busybox-extras
COPY probe /probe
COPY vendor-mock /usr/local/bin/vendor-mock
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh /usr/local/bin/vendor-mock
ENTRYPOINT ["/entrypoint.sh"]
