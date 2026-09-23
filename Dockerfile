FROM golang:1.26.8-bookworm AS source
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

FROM source AS test
CMD ["go", "test", "-race", "-tags=integration", "-count=1", "./..."]

FROM source AS build
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOBIN=/out go install -trimpath -ldflags="-s -w" \
    -tags="no_clickhouse,no_libsql,no_mssql,no_mysql,no_sqlite3,no_vertica,no_ydb" \
    github.com/pressly/goose/v3/cmd/goose@v3.28.0

FROM scratch AS runtime
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/api /api
COPY --from=build /out/goose /goose
COPY migrations/*.sql /migrations/
COPY config.example.yaml /config.yaml
WORKDIR /
USER 65532:65532
EXPOSE 8080
CMD ["/api"]
