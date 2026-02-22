# Third-Party Notices

This repository is licensed under AGPL-3.0-or-later for Grimnir Radio source code.

Third-party components included in this repository or container images remain under
their own licenses. The AGPL license for Grimnir Radio does not replace or
relicense third-party code.

## Bundled Frontend Assets

The following third-party frontend assets are bundled under `internal/web/static/`:

- `htmx.min.js` (htmx) - 0BSD - `third_party/licenses/HTMX-LICENSE.txt`
- `alpine.min.js` (Alpine.js) - MIT - `third_party/licenses/ALPINEJS-LICENSE.txt`
- `bootstrap.min.css` and `bootstrap.bundle.min.js` (Bootstrap) - MIT - `third_party/licenses/BOOTSTRAP-LICENSE.txt`
- `bootstrap-icons.min.css` and fonts (Bootstrap Icons) - MIT - `third_party/licenses/BOOTSTRAP-ICONS-LICENSE.txt`
- `fullcalendar.min.js` (FullCalendar) - MIT - `third_party/licenses/FULLCALENDAR-LICENSE.txt`

## Go Module Dependency Licenses

- Machine-generated dependency report: `third_party/go-licenses.csv`
- Generated with:

```bash
go run github.com/google/go-licenses@latest report ./... > third_party/go-licenses.csv
```

## Container Images

Published GHCR images include:

- `LICENSE`
- `THIRD_PARTY_NOTICES.md`
- `third_party/licenses/*`
- `third_party/go-licenses.csv`

These files are installed under `/usr/share/licenses/grimnir-radio/` in the image.
