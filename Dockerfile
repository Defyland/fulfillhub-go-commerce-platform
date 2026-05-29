FROM golang:1.23.3-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN go build -o /out/fulfillhub-api ./cmd/fulfillhub-api

FROM alpine:3.20

RUN addgroup -S fulfillhub && adduser -S fulfillhub -G fulfillhub
USER fulfillhub
WORKDIR /app
COPY --from=build /out/fulfillhub-api /app/fulfillhub-api
EXPOSE 8080

ENTRYPOINT ["/app/fulfillhub-api"]
