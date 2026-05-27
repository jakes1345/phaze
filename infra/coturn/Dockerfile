# Phaze TURN/STUN server — coturn on Alpine, configured for time-limited
# credentials (HMAC-SHA1) so clients never see a long-lived secret.

FROM alpine:3.20

RUN apk add --no-cache coturn

COPY --chown=turnserver:turnserver entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

USER turnserver

EXPOSE 3478/udp 3478/tcp 5349/tcp 49160-49200/udp

ENTRYPOINT ["/entrypoint.sh"]
