# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/codepeer ./cmd/codepeer

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/codepeer /codepeer
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/codepeer"]
