# Security Policy

## Supported Versions

| Version   | Supported |
|-----------|-----------|
| 1.17.x    | Yes       |
| < 1.17    | No        |

Only the latest minor release receives security patches. We recommend always running the most recent version.

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Instead, report vulnerabilities privately using one of these methods:

1. **GitHub Security Advisories** (preferred): Use the "Report a vulnerability" button on the [Security tab](https://github.com/friendsincode/grimnir_radio/security/advisories/new) of this repository.
2. **Email**: Send details to **friendsincode@proton.me**

### What to include

- Description of the vulnerability
- Steps to reproduce or proof of concept
- Affected versions
- Potential impact

### What to expect

- **Acknowledgement** within 48 hours
- **Initial assessment** within 7 days
- **Fix or mitigation** for confirmed vulnerabilities within 30 days, depending on severity
- Credit in the release notes (unless you prefer to remain anonymous)

## Scope

The following areas are in scope for security reports:

- Authentication and authorization (JWT, RBAC, API keys)
- Session management and CSRF protections
- Media upload and file handling
- gRPC communication between control plane and media engine
- WebSocket and SSE endpoints
- Database injection (SQL, GORM)
- Live DJ input handling (Icecast, RTP, SRT, WebRTC)
- Webstream relay and health check mechanisms
- Docker deployment configuration

## Disclosure Policy

We follow coordinated disclosure:

1. Reporter submits vulnerability privately
2. We confirm and develop a fix
3. Fix is released with a security advisory
4. Public disclosure after the fix is available

We ask reporters to allow up to 90 days before public disclosure to give deployers time to update.

## Security Best Practices for Deployers

- Always set a strong `GRIMNIR_JWT_SIGNING_KEY` (minimum 32 characters)
- Use HTTPS in production (configure a reverse proxy with TLS)
- Keep the `GRIMNIR_MEDIA_ROOT` directory outside the web root
- Restrict access to the gRPC port (default 9091) to internal networks only
- Run containers with read-only root filesystem where possible
- Regularly update to the latest release
