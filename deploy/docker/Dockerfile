# One image holds every backend service plus the built web app; compose picks the command per service.
# Build from the repository root:  docker build -f deploy/docker/Dockerfile .

FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.22-bookworm AS build
RUN apt-get update && apt-get install -y --no-install-recommends protobuf-compiler libprotobuf-dev make \
    && rm -rf /var/lib/apt/lists/*
# generated protobuf code is not committed, so it is generated here with pinned plugins
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2 \
    && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
WORKDIR /src
COPY proto proto
COPY backend backend
WORKDIR /src/backend
RUN make proto && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /usr/local/bin/
COPY --from=web /web/dist /web
ENV PATH=/usr/local/bin WEB_DIR=/web
USER nonroot:nonroot
