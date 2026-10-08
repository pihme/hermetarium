# Build context is a directory that contains the echo-service binary.
# `make echo-image` builds it and tags hermetarium/echo:dev.
FROM alpine:3.24
RUN apk add --no-cache curl iproute2 iptables
COPY echo-service /usr/local/bin/echo-service
EXPOSE 8080
CMD ["/usr/local/bin/echo-service"]
