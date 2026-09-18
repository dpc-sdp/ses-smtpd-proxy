# Multi-stage build for ses-smtpd-proxy.
# The arm64 builder avoids segfaults when cross-compiling amd64 images.
FROM --platform=linux/arm64 golang:alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=latest
ARG TARGETPLATFORM
RUN GOOS="$(echo "${TARGETPLATFORM}" | cut -d/ -f1)" \
    GOARCH="$(echo "${TARGETPLATFORM}" | cut -d/ -f2)" \
    CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /ses-smtpd-proxy .

FROM gcr.io/distroless/static:nonroot AS runtime

# 2500 - SMTP protocol
# 2501 - Prometheus metrics endpoint
# 3000 - Health check endpoint
EXPOSE 2500 2501 3000

COPY --from=builder --chown=nonroot:nonroot /ses-smtpd-proxy /ses-smtpd-proxy
USER nonroot
ENTRYPOINT ["/ses-smtpd-proxy"]
CMD ["0.0.0.0:2500"]
