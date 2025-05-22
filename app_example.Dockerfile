FROM --platform=$BUILDPLATFORM golang:1.24 AS builder

ARG TARGETARCH=amd64
ARG VERSION=devbuild
ARG COMMIT

WORKDIR /build

COPY go.* ./
RUN go mod download
COPY . .
RUN mkdir bin && CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="-s -w -X 'github.com/sedmess/go-ctx/ctx/appinfo.Version=$VERSION' -X 'github.com/sedmess/go-ctx/ctx/appinfo.BuildInfo=build $COMMIT ($(date)'" -o ./bin/app-$TARGETARCH ./

FROM alpine:3.21

ARG TARGETARCH=amd64
ARG VERSION=devbuild
ARG DATE
ARG COMMIT
ARG UID=65501

LABEL version="$VERSION" buildDate="$DATE" buildCommit="$COMMIT" gid="$UID" uid="$UID"

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /build/bin/app-$TARGETARCH /app/app

RUN adduser -u "$UID" -s /sbin/nologin -D app && chown "$UID:$UID" /app/app

USER app

ENTRYPOINT ["/app/app"]
