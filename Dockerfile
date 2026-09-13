FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/local-dist ./cmd/local-dist

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/local-dist /local-dist
EXPOSE 8080
ENTRYPOINT ["/local-dist"]
CMD ["-listen", ":8080", "-data", "/data"]
