FROM golang:1.23.3-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN go build -o /out/fulfillhub-api ./cmd/fulfillhub-api
RUN go build -o /out/fulfillhub-dlq-replay ./cmd/fulfillhub-dlq-replay
RUN go build -o /out/fulfillhub-outbox-relay ./cmd/fulfillhub-outbox-relay

FROM alpine:3.20

RUN addgroup -S fulfillhub && adduser -S fulfillhub -G fulfillhub
USER fulfillhub
WORKDIR /app
COPY --from=build /out/fulfillhub-api /app/fulfillhub-api
COPY --from=build /out/fulfillhub-dlq-replay /app/fulfillhub-dlq-replay
COPY --from=build /out/fulfillhub-outbox-relay /app/fulfillhub-outbox-relay
EXPOSE 8080

ENTRYPOINT ["/app/fulfillhub-api"]
