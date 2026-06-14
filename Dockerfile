FROM gcr.io/distroless/static-debian12
COPY go-sheet-entry-linux-arm64 /go-sheet-entry
WORKDIR /data
ENTRYPOINT ["/go-sheet-entry"]
