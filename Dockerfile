# SPDX-License-Identifier: AGPL-3.0-or-later

FROM golang:1.25-alpine AS build
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/Work-Fort/sharkfin/cmd.Version=${VERSION}" \
    -o /sharkfin

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /sharkfin /usr/local/bin/sharkfin
ENTRYPOINT ["sharkfin"]
CMD ["daemon"]
