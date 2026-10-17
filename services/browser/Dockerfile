# The Playwright image carries Chromium and its system libraries at the
# version playwright-core 1.56.1 drives; keep the two in step.
FROM mcr.microsoft.com/playwright:v1.56.1-noble

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev
COPY src ./src

ENV BROWSER_PORT=7070 \
    BROWSER_PROFILES_DIR=/data/profiles \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
RUN mkdir -p /data/profiles && chown pwuser:pwuser /data/profiles
VOLUME /data/profiles
EXPOSE 7070
USER pwuser
CMD ["node", "src/main.js"]
