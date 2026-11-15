FROM alpine:3.24
# Strong attach is SSH, so the freeze needs sshd. fsfreeze is util-linux.
RUN apk add --no-cache openssh util-linux
COPY start.sh /start.sh
RUN chmod +x /start.sh
ENTRYPOINT ["/start.sh"]
