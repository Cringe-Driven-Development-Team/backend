FROM golang:1.27.1-alpine AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM go-builder AS identity-builder
RUN go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/identity ./cmd/identity-admin

FROM go-builder AS workspace-builder
RUN go build -trimpath -ldflags="-s -w" -o /out/workspace ./cmd/workspace

FROM go-builder AS environments-builder
RUN go build -trimpath -ldflags="-s -w" -o /out/environments ./cmd/environments

FROM go-builder AS onlineconf-builder
RUN go build -trimpath -ldflags="-s -w" -o /out/onlineconf ./cmd/onlineconf

FROM python:3.13-slim-bookworm AS agent-builder
WORKDIR /src
COPY runtime-agent/ ./
RUN pip wheel --no-cache-dir --no-deps --wheel-dir /out . \
    && cd /out \
    && set -- cellestial_agent-*.whl \
    && test "$#" -eq 1 \
    && checksum="$(sha256sum "$1")" \
    && printf '%s\n' "${checksum%% *}" > "$1.sha256"

FROM alpine:3.23 AS service-runtime
RUN adduser -D -u 10001 app
WORKDIR /app
COPY --from=go-builder /out/migrate ./migrate

FROM service-runtime AS identity
COPY --from=identity-builder /out/identity /out/identity-admin ./
USER app
EXPOSE 8080 9090 9091
CMD ["./identity"]

FROM service-runtime AS workspace
COPY --from=workspace-builder /out/workspace ./workspace
USER app
EXPOSE 8080 9090 9091
CMD ["./workspace"]

FROM service-runtime AS onlineconf
COPY --from=onlineconf-builder /out/onlineconf ./onlineconf
USER app
EXPOSE 8080 9091
CMD ["./onlineconf"]

FROM service-runtime AS environments
ARG RUNTIME_PYTHON_IMAGE=cellestial-kernel-python:local
ARG RUNTIME_AGENT_VERSION=dev
ENV RUNTIME_PYTHON_IMAGE=$RUNTIME_PYTHON_IMAGE \
    RUNTIME_AGENT_VERSION=$RUNTIME_AGENT_VERSION \
    RUNTIME_WORK_DIR=/var/lib/cellestial/runtime
RUN apk add --no-cache python3 ansible-core openssh-client \
    && install -d -m 0700 -o app -g app "$RUNTIME_WORK_DIR" \
    && mkdir -p /app/deploy/runtime
COPY --from=environments-builder /out/environments ./environments
COPY --from=agent-builder /out/ ./agent/
COPY runtime-agent/requirements.txt runtime-agent/deploy/cellestial-agent.service runtime-agent/deploy/cellestial-agent-grpc.service ./agent/
USER app
EXPOSE 9090 9091
CMD ["./environments"]
