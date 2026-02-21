# HTTP Security Headers

Grimnir sets baseline security headers in-app via `securityHeadersMiddleware` (`internal/server/server.go`):

- `Content-Security-Policy`
- `X-Frame-Options: DENY`
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Strict-Transport-Security` (only when request is HTTPS, detected via TLS or `X-Forwarded-Proto=https`)

## Reverse Proxy Contract

- The reverse proxy/ingress must forward the original scheme using `X-Forwarded-Proto` for HSTS to be emitted on TLS-terminated deployments.
- Proxy-level header injection may still be used, but app-level defaults are enforced regardless.

## Test Coverage

- `internal/server/security_headers_test.go` validates baseline headers and HTTPS-only HSTS behavior.
