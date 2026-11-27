# syntax=docker/dockerfile:1

ARG NODE_IMAGE
ARG GO_IMAGE
ARG RUNTIME_IMAGE

FROM ${NODE_IMAGE} AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM ${GO_IMAGE} AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY app/ ./app/
COPY platform/ ./platform/
COPY --from=web-build /src/platform/webui/dist/ ./platform/webui/dist/
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/platform \
    ./cmd/platform

FROM ${RUNTIME_IMAGE}
COPY --from=go-build /out/platform /platform
USER 65532:65532
EXPOSE 8080 8081
ENTRYPOINT ["/platform"]
