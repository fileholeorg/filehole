# Filehole

Filehole is a lightweight ephemeral file hosting service written in Go.

![A pic of my hole](docs/filehole.png)

## Installation

### Go

To install Filehole, use the following command:

```bash
go install github.com/fileholeorg/filehole@latest
```

This will download and install the latest version of Filehole.

### Docker

There is a container, you will have to build it yourself for now.

`docker build -t filehole:latest .`

Bind mounting `/data` and `/buffer` recommended if you want files to persist restarts and upgrades.

## Configuration

Filehole can be configured using environment variables or arguments.

    -bind string
        Address to bind ENV: FH_BIND (default "127.0.0.1:8000")
    -buffer-dir string
        Buffer folder for uploads ENV: FH_STORAGE_DIR (default "./buffer")
    -csp-off
        Disable Content-Security-Policy nonces ENV: FH_CSP_OFF
    -debug
        Enable debug logging for development ENV: FH_DEBUG
    -metadata-path string
        File metadata storage KV store filename ENV: FH_METADATA_FILE (default "./filehole.db")
    -public-url string
        Internet facing URL of the base of the site ENV: FH_PUBLIC_URL (default "https://filehole.org")
    -serve-url string
        Internet facing URL of the base of uploads, only for using a CDN, object storage, etc. ENV: FH_SERVE_URL (default "https://filehole.org")
    -site-name string
        User facing website branding ENV: FH_SITE_NAME (default "Filehole")
    -storage-dir string
        Data storage folder ENV: FH_STORAGE_DIR (default "./data")
    -upload-limit int
        Max allowed size for a HTTP request in bytes ENV: FH_UPLOAD_LIMIT (default 1073741824)

## Dependencies used

### Frontend

- [Pico CSS](https://picocss.com/)

### Frontend (with Javascript enabled)

- [jQuery](https://jquery.com/)

### Backend

- [rs/zerolog](https://github.com/rs/zerolog)
  Used for logging. Should be replaced with [log/slog](https://pkg.go.dev/log/slog) now.

- [boltdb/bolt](https://github.com/boltdb/bolt)
  Used to store expiry dates of files. Tempted to just use file system creation time but this is not universal.

- [gorilla/mux](https://github.com/gorilla/mux)
  Can maybe be replaced with [net/http](https://go.dev/blog/routing-enhancements) now.

- [gabriel-vasile/mimetype](https://github.com/gabriel-vasile/mimetype)
  The function used from this lib is present in stdlib but a bit slower there
