# syntax=docker/dockerfile:1

# ---- build stage --------------------------------------------------------
FROM golang:1.26 AS build
WORKDIR /src

# Cache de dependências: copia só os manifests primeiro.
COPY go.mod go.sum ./
RUN go mod download

# Código-fonte e build estático.
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/cubodw ./cmd/cubodw

# ---- runtime stage ------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cubodw /usr/local/bin/cubodw
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/cubodw"]
CMD ["serve-engine"]
