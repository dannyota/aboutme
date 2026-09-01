FROM mcr.microsoft.com/playwright:v1.62.1-noble@sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac

LABEL io.aboutme.dev-https-browser.contract="2" \
      io.aboutme.dev-https-browser.base="mcr.microsoft.com/playwright:v1.62.1-noble@sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac" \
      io.aboutme.dev-https-browser.playwright="1.62.1" \
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
COPY run.sh ./
RUN chmod 0444 package.json package-lock.json \
    && chmod 0555 run.sh

USER pwuser

ENTRYPOINT ["/opt/aboutme-auth/run.sh", "--inside"]
