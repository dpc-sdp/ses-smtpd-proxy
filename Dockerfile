# Multi-stage build for ses-smtpd-proxy.
# The arm64 builder avoids segfaults when cross-compiling amd64 images.
FROM --platform=linux/arm64 golang:1.24.2-alpine@sha256:7772cb5322baa875edd74705556d08f0eeca7b9c4b5367754ce3f2f00041ccee AS builder
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

FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 AS runtime

# 2500 - SMTP protocol
# 2501 - Prometheus metrics endpoint
# 3000 - Health check endpoint
EXPOSE 2500 2501 3000

COPY --from=builder --chown=nonroot:nonroot /ses-smtpd-proxy /ses-smtpd-proxy
USER nonroot
ENTRYPOINT ["/ses-smtpd-proxy"]
CMD ["0.0.0.0:2500"]
