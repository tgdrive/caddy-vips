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
            quality 82
            max_dimension 8192
            max_pixels 40000000
            max_source_size 64MiB
            debug_headers off
        }

        varc https://origin.example.com {
            cache_dir /var/cache/caddy/varc
        }
    }
}
```

Supported query parameters include `w`, `h`, `fit`, `gravity`, `q`, `format`,
`dpr`, `rotate`, `flip`, `without_enlargement`, and `background`.
