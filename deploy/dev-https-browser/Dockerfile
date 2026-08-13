FROM mcr.microsoft.com/playwright:v1.62.1-noble@sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac

USER root

RUN apt-get update \
    && apt-get install -y --no-install-recommends libnss3-tools=2:3.98-1ubuntu0.2 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/aboutme-auth

COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts \
    && npm cache clean --force

COPY playwright.config.ts auth.spec.ts run.sh ./
RUN chmod 0444 package.json package-lock.json playwright.config.ts auth.spec.ts \
    && chmod 0555 run.sh

USER pwuser

ENTRYPOINT ["/opt/aboutme-auth/run.sh", "--inside"]
