# KeLai AGI worker: sandbox for terminal, file and browser tools. Telegram is never
# reached from here; the backend owns that session.
FROM python:3.12-slim-bookworm

ENV DEBIAN_FRONTEND=noninteractive \
    PIP_NO_CACHE_DIR=1 \
    PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl jq postgresql-client git procps less file unzip \
    && rm -rf /var/lib/apt/lists/*

RUN pip install \
        requests beautifulsoup4 lxml pandas \
        langdetect jieba rapidfuzz \
        playwright \
    && playwright install --with-deps chromium \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /work
