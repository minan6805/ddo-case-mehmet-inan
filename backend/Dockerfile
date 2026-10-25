FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api

FROM build AS test
RUN apk add --no-cache build-base
ENTRYPOINT ["go", "test", "-race", "-count=1", "./..."]

FROM build AS simulator-build
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/simulator ./cmd/simulator

FROM alpine:3.21 AS simulator
RUN apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=simulator-build /out/simulator /app/simulator
USER app
ENTRYPOINT ["/app/simulator"]

FROM alpine:3.21 AS api
RUN apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/api /app/api
USER app
EXPOSE 8080
ENTRYPOINT ["/app/api"]
