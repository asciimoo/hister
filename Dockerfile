FROM golang:1.24-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

# Switch workdir do build directory
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Enable CGO and build the application for Linux
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w" \
    -o hister .

# Release stage(distroless-nonroot)
# latest & vx.x.x
FROM gcr.io/distroless/base-debian12:nonroot AS release
WORKDIR /hister

COPY --from=builder /app/hister .

USER 65532:65532

ENV HISTER_DATA_DIR=/hister/data
ENV HISTER_CONFIG=/hister/data/config.yml
ENV HISTER__SERVER__ADDRESS=0.0.0.0:4433
ENV HISTER__SERVER__BASE_URL=http://localhost:4433

EXPOSE 4433

ENTRYPOINT ["/hister/hister"]
CMD ["listen"]

# Release stage(distroless)
# latest-root & vx.x.x-root
FROM gcr.io/distroless/base-debian12 AS root
WORKDIR /hister

COPY --from=builder /app/hister .

USER root

ENV HISTER_DATA_DIR=/hister/data
ENV HISTER_CONFIG=/hister/data/config.yml

EXPOSE 4433

ENTRYPOINT ["/hister/hister"]
CMD ["listen"]

# Release stage(distroless-debug)
# latest-debug & vx.x.x-debug
FROM gcr.io/distroless/base-debian12:debug AS debug
WORKDIR /hister

COPY --from=builder /app/hister .

USER root

ENV HISTER_DATA_DIR=/hister/data
ENV HISTER_CONFIG=/hister/data/config.yml

EXPOSE 4433

ENTRYPOINT ["/hister/hister"]
CMD ["listen"]
