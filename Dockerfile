# Etapa de build: usa toolchain oficial de Go para compilar el binario
FROM golang:1.22 AS build
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
# Binario estático para runtime mínimo
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

# Etapa de runtime: imagen mínima sin intérpretes ni shell
FROM gcr.io/distroless/static:nonroot
WORKDIR /app
USER nonroot:nonroot
COPY --from=build /server /app/server
EXPOSE 8080
ENTRYPOINT ["/app/server"]
