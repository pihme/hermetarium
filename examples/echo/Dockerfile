# Context is the directory that contains echo-service (supervisor writes it to .cache/).
FROM alpine:3.20
RUN apk add --no-cache curl iproute2 iptables
COPY echo-service /usr/local/bin/echo-service
EXPOSE 8080
CMD ["/usr/local/bin/echo-service"]
