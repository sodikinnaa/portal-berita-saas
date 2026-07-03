# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/portal ./cmd/portal

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/portal /app/portal
ENV ADDR=:8080
ENV UPLOAD_DIR=/tmp/uploads
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/portal"]
