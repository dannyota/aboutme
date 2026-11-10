# Published as ghcr.io/dannyota/aboutme-browser-base so CI never runs apt-get
# against live Ubuntu mirrors. Rebuild and republish only when the Playwright
# base or the libnss3-tools pin changes; see instructions/verification.md.
FROM mcr.microsoft.com/playwright:v1.64.0-noble@sha256:bc72a8df40831ded821e3cbc79c318c21b44d6e51dff9fd89ace7a8249c5b462

LABEL org.opencontainers.image.source="https://github.com/dannyota/aboutme" \
      org.opencontainers.image.description="HTTPS proof browser base: Playwright 1.64.0 noble with libnss3-tools 2:3.98-1ubuntu0.3" \
      io.aboutme.dev-https-browser-base.playwright="1.64.0" \
      io.aboutme.dev-https-browser-base.libnss3-tools="2:3.98-1ubuntu0.3"

USER root

RUN apt-get -o Acquire::Retries=5 update \
    && apt-get install -y --no-install-recommends libnss3-tools=2:3.98-1ubuntu0.3 \
    && rm -rf /var/lib/apt/lists/*
