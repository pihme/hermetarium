FROM alpine:3.24
# The backup freezes /data through hermetarium attach.
RUN apk add --no-cache util-linux
COPY start.sh /start.sh
RUN chmod +x /start.sh
ENTRYPOINT ["/start.sh"]
