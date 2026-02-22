# Licensing Compliance (GHCR)

This project publishes container images to GHCR only.

## What is shipped

Each image should contain:

- `/usr/share/licenses/grimnir-radio/LICENSE`
- `/usr/share/licenses/grimnir-radio/THIRD_PARTY_NOTICES.md`
- `/usr/share/licenses/grimnir-radio/third_party/go-licenses.csv`
- `/usr/share/licenses/grimnir-radio/third_party/licenses/*`

## Release artifacts

On tag builds (`v*`), the release workflow attaches:

- `LICENSE`
- `THIRD_PARTY_NOTICES.md`
- `third_party/go-licenses.csv`
- `grimnir-radio-licenses-<tag>.tar.gz` (license bundle)

## Verification commands

```bash
docker pull ghcr.io/friendsincode/grimnir_radio:<tag>
docker run --rm --entrypoint ls ghcr.io/friendsincode/grimnir_radio:<tag> \
  -R /usr/share/licenses/grimnir-radio
```

```bash
docker pull ghcr.io/friendsincode/grimnir_mediaengine:<tag>
docker run --rm --entrypoint ls ghcr.io/friendsincode/grimnir_mediaengine:<tag> \
  -R /usr/share/licenses/grimnir-radio
```
