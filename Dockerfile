##################################
# Stage 0: Build frontend module
##################################

FROM node:20-alpine AS frontend-builder

RUN npm install -g pnpm@9

WORKDIR /frontend
COPY frontend/package.json frontend/pnpm-lock.yaml* ./
RUN pnpm install --frozen-lockfile || pnpm install
COPY frontend/ .
RUN pnpm build

##################################
# Stage 1: Build Go executable
##################################

FROM golang:1.23-alpine AS builder

ARG APP_VERSION=1.0.0

ENV GOTOOLCHAIN=auto

RUN apk add --no-cache git make curl

# Install buf for proto descriptor generation
RUN curl -sSL "https://github.com/bufbuild/buf/releases/latest/download/buf-$(uname -s)-$(uname -m)" -o /usr/local/bin/buf && \
    chmod +x /usr/local/bin/buf

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Regenerate proto descriptor
RUN buf build -o cmd/server/assets/descriptor.bin

# Copy frontend dist into assets for go:embed
COPY --from=frontend-builder /frontend/dist cmd/server/assets/frontend-dist/

# Build the server
RUN CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    go build -ldflags "-X main.version=${APP_VERSION} -s -w" \
    -o /src/bin/notification-server \
    ./cmd/server

##################################
# Stage 2: Create runtime image
##################################

FROM alpine:3.20

ARG APP_VERSION=1.0.0

RUN apk --no-cache add ca-certificates tzdata

ENV TZ=UTC

WORKDIR /app

COPY --from=builder /src/bin/notification-server /app/bin/notification-server
COPY --from=builder /src/configs/ /app/configs/

RUN addgroup -g 1000 notification && \
    adduser -D -u 1000 -G notification notification && \
    chown -R notification:notification /app

USER notification:notification

EXPOSE 10300 10301

CMD ["/app/bin/notification-server", "-c", "/app/configs"]

LABEL org.opencontainers.image.title="Notification Service" \
      org.opencontainers.image.description="Multi-channel notification service with template-based messaging" \
      org.opencontainers.image.version="${APP_VERSION}"
