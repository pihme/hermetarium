FROM docker:29-dind
COPY busybox.tar /images/busybox.tar
COPY start.sh /start.sh
RUN chmod +x /start.sh
ENV DOCKER_TLS_CERTDIR=
ENV DOCKER_INSECURE_NO_IPTABLES_RAW=1
ENTRYPOINT ["/start.sh"]
