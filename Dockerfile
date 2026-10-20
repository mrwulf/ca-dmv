FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /out/ca-dmv . \
 && mkdir /out/data

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/ca-dmv /ca-dmv
# Empty writable data dir for when no volume is mounted; mount a PVC at /data to keep history.
COPY --from=build --chown=65532:65532 /out/data /data
ENV DB_PATH=/data/ca-dmv.db
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/ca-dmv"]
