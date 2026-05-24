# Phaze TURN/STUN server — coturn, configured for time-limited credentials
# (HMAC-SHA1 over expiry+username) so clients never see a long-lived secret.
# Listens on 3478/UDP+TCP for STUN/TURN and 5349/TCP for TURN-over-TLS.

FROM coturn/coturn:4.7-alpine

# Run as the non-root user the upstream image creates.
USER turnserver

# Config is supplied via env vars at boot, see entrypoint.
COPY --chown=turnserver:turnserver entrypoint.sh /entrypoint.sh
USER root
RUN chmod +x /entrypoint.sh
USER turnserver

EXPOSE 3478/udp 3478/tcp 5349/tcp 49160-49200/udp

ENTRYPOINT ["/entrypoint.sh"]
