# caddy-vips

A Caddy HTTP middleware for libvips-backed image resizing and derivative caching.
It transforms the response produced by the next handler, so it can be composed with
`varc`, `file_server`, `reverse_proxy`, or another Caddy content handler.

Build Caddy with this module; libvips support is always enabled:

```sh
xcaddy build \
  --with github.com/tgdrive/caddy-vips \
  --with github.com/tgdrive/varc
```

Example:

```caddyfile
:8080 {
    route /images/* {
        vips {
            cache_dir /var/cache/caddy/vips
            cache_max_size 100GiB
            quality 82
            max_dimension 8192
            max_pixels 40000000
            max_source_size 64MiB
            debug_headers off
            enable_logs off
        }

        varc https://origin.example.com {
            cache_dir /var/cache/caddy/varc
        }
    }
}
```

`enable_logs` controls libvips and go-vips log output. It is disabled by default;
set it to `on` only when troubleshooting.

Supported query parameters include `w`, `h`, `fit`, `gravity`, `q`, `format`,
`dpr`, `rotate`, `flip`, `without_enlargement`, and `background`.

## Immutable source URLs

Derivative cache keys use the normalized source request URI plus the transform specification. For best performance and correct invalidation, each source URL must identify immutable image bytes, for example `/images/<image-id>`. When an image changes, publish it under a new ID instead of replacing the bytes behind an existing URL. This lets derivative cache hits bypass the downstream source handler entirely.


## Cache eviction

Set `cache_max_size` to bound derivative disk usage. Cache hits update file access timestamps, and after each new derivative write the cache removes the least-recently-used files until total usage is within the configured limit. A zero value leaves the cache unbounded.
