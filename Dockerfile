FROM golang:1.25.10-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN go build -o /out/fulfillhub-api ./cmd/fulfillhub-api
RUN go build -o /out/fulfillhub-dlq-replay ./cmd/fulfillhub-dlq-replay
RUN go build -o /out/fulfillhub-outbox-relay ./cmd/fulfillhub-outbox-relay
RUN go build -o /out/fulfillhub-worker ./cmd/fulfillhub-worker

FROM alpine:3.20

RUN addgroup -S fulfillhub && adduser -S fulfillhub -G fulfillhub
USER fulfillhub
WORKDIR /app
COPY --from=build /out/fulfillhub-api /app/fulfillhub-api
COPY --from=build /out/fulfillhub-dlq-replay /app/fulfillhub-dlq-replay
COPY --from=build /out/fulfillhub-outbox-relay /app/fulfillhub-outbox-relay
COPY --from=build /out/fulfillhub-worker /app/fulfillhub-worker
EXPOSE 8080

CMD ["/app/fulfillhub-api"]
