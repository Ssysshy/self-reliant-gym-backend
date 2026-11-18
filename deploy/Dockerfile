FROM node:22-alpine AS build-web
WORKDIR /src
RUN corepack enable
COPY apps/frontend/package.json apps/frontend/pnpm-lock.yaml ./
RUN corepack pnpm install --frozen-lockfile
COPY apps/frontend/ .
RUN corepack pnpm exec tsc --noEmit && corepack pnpm exec vite build --outDir /web

FROM nginx:alpine AS web
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build-web /web /usr/share/nginx/html

FROM golang:1.25-alpine AS build-api
WORKDIR /src
COPY apps/backend/go.* ./
RUN go mod download
COPY apps/backend/ .
RUN CGO_ENABLED=0 go build -mod=mod -trimpath -o /gym ./cmd/api

FROM alpine:3 AS api
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build-api /gym /usr/local/bin/gym
# 内置动作库与官方模板，随镜像带上作参考数据，启动不读
COPY seed /app/seed
USER nobody
CMD ["gym"]
