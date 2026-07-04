FROM golang:1.23-bookworm AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /out/ai-usage-dashboard ./cmd/ai-usage-dashboard

FROM debian:bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=build /out/ai-usage-dashboard /app/ai-usage-dashboard
VOLUME ["/data"]
ENV CUA_ADDR=:4318
ENV CUA_DB=/data/ai-usage-dashboard.sqlite
EXPOSE 4318

ENTRYPOINT ["/app/ai-usage-dashboard"]
