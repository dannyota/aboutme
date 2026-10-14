FROM mcr.microsoft.com/playwright:v1.64.0-noble@sha256:bc72a8df40831ded821e3cbc79c318c21b44d6e51dff9fd89ace7a8249c5b462

LABEL io.aboutme.dev-https-browser.contract="2" \
      io.aboutme.dev-https-browser.base="mcr.microsoft.com/playwright:v1.64.0-noble@sha256:bc72a8df40831ded821e3cbc79c318c21b44d6e51dff9fd89ace7a8249c5b462" \
      io.aboutme.dev-https-browser.playwright="1.64.0" \
      io.aboutme.dev-https-browser.libnss3-tools="2:3.98-1ubuntu0.2"

USER root

RUN apt-get update \
    && apt-get install -y --no-install-recommends libnss3-tools=2:3.98-1ubuntu0.2 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/aboutme-auth

COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts \
    && npm cache clean --force

# Spec sources are NOT baked in; they are mounted read-only at /uat-spec per
# run and validated by run.sh, so a spec edit needs no image rebuild.
COPY run.sh verify-evidence.mjs spec-sources.txt ./
RUN chmod 0444 package.json package-lock.json verify-evidence.mjs spec-sources.txt \
    && chmod 0555 run.sh

USER pwuser

ENTRYPOINT ["/opt/aboutme-auth/run.sh", "--inside"]
