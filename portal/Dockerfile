# syntax=docker/dockerfile:1
# The platform portal (Backstage), built entirely inside Docker so the platform's golden path
# (build-app.yml) can build, scan and sign it like any app: docker build -t portal-web .
# Nothing platform-specific is baked in: modules/portal mounts app-config.platform.yaml.

# --- Build: install, typecheck, bundle the backend (it serves the frontend too) --------------------
FROM node:24-trixie-slim AS build
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends python3 g++ build-essential libsqlite3-dev && \
    rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY . .
RUN --mount=type=cache,target=/root/.yarn/berry/cache \
    yarn install --immutable
RUN yarn tsc && yarn build:backend

# --- Runtime ---------------------------------------------------------------------------------------
FROM node:24-trixie-slim
# Security updates for the base image, and TechDocs, generated inside the portal
# (techdocs.generator.runIn: local): mkdocs in a venv.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && apt-get upgrade -y && \
    apt-get install -y --no-install-recommends libsqlite3-dev python3 python3-venv && \
    rm -rf /var/lib/apt/lists/* && \
    python3 -m venv /opt/mkdocs && \
    /opt/mkdocs/bin/pip install --no-cache-dir mkdocs-techdocs-core==1.7.1
ENV PATH=/opt/mkdocs/bin:$PATH \
    NODE_ENV=production
WORKDIR /app
COPY .yarn ./.yarn
COPY .yarnrc.yml backstage.json yarn.lock package.json ./
COPY --from=build /app/packages/backend/dist/skeleton.tar.gz ./
RUN tar xzf skeleton.tar.gz && rm skeleton.tar.gz
RUN yarn workspaces focus --all --production && rm -rf "$(yarn cache clean)"
COPY --from=build /app/packages/backend/dist/bundle.tar.gz ./
COPY app-config.yaml app-config.production.yaml ./
RUN tar xzf bundle.tar.gz && rm bundle.tar.gz && \
    # The base image's npm (and its bundled dependencies) isn't used at runtime: Yarn installed
    # everything above. Removing it removes its vulnerabilities from the image.
    rm -rf /usr/local/lib/node_modules/npm /usr/local/bin/npm /usr/local/bin/npx && \
    chown -R 1000:1000 /app
# Numeric user (node): Pod Security "restricted" needs runAsNonRoot, checkable only for a UID.
USER 1000:1000
EXPOSE 7007
CMD ["node", "packages/backend", "--config", "app-config.yaml", "--config", "app-config.production.yaml", "--config", "/app/config/app-config.platform.yaml"]
