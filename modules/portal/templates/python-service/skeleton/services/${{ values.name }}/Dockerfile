# syntax=docker/dockerfile:1
# ${{ values.name }}: one image (<name>-web). docker build -t ${{ values.name }}-web .

# --- Dependencies: a virtualenv from the hash-pinned requirements --------------------------------
FROM python:3.14-slim AS build
WORKDIR /app
COPY requirements.txt ./
# No pip inside the venv: the image's pip installs into it (--python), so the runtime image
# carries none of pip's vendored packages.
RUN python -m venv --without-pip /venv && \
    pip --python /venv/bin/python install --no-cache-dir --require-hashes -r requirements.txt

# --- Runtime -------------------------------------------------------------------------------------
FROM python:3.14-slim
ARG VERSION=dev
# Security updates for the base image. pip and ensurepip's bundled wheels aren't needed at
# runtime (the venv is already built), so they go, with their vulnerabilities.
RUN apt-get update && apt-get upgrade -y && rm -rf /var/lib/apt/lists/* && \
    python -m pip uninstall -y pip && \
    rm -rf /usr/local/lib/python3*/ensurepip
WORKDIR /srv
COPY --from=build /venv /venv
COPY app ./app
ENV PATH=/venv/bin:$PATH \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    VERSION=$VERSION
# Numeric user: Pod Security "restricted" checks runAsNonRoot against a UID.
USER 65532:65532
EXPOSE 8080 9090
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8080", "--no-server-header"]
