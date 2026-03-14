# SPDX-License-Identifier: AGPL-3.0-or-later

FROM alpine:3.21 AS build
RUN apk add --no-cache bash git \
    && apk add --no-cache --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community mise
SHELL ["/bin/bash", "-c"]
WORKDIR /src
COPY mise.toml ./
COPY .mise/ .mise/
RUN mise trust && mise install
COPY go.mod go.sum ./
RUN eval "$(mise activate bash)" && go mod download
COPY . .
ARG VERSION=dev
RUN eval "$(mise activate bash)" && VERSION=${VERSION} mise run build:release

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /src/build/sharkfin /usr/local/bin/sharkfin
ENTRYPOINT ["sharkfin"]
CMD ["daemon"]
