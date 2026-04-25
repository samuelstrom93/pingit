FROM node:20-alpine AS frontend
WORKDIR /app/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.23-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/build ./web/build
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /pingit ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=backend /pingit /pingit
ENV PINGIT_ENV=prod
ENV PINGIT_ADDR=:8080
ENV PINGIT_DB=/data/pingit.db
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/pingit"]

