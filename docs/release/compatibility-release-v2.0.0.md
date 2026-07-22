# Compatibility release v2.0.0 evidence

The first complete local compatibility release was published on 2026-07-22 while the legacy Web and remote MCP services remained online.

## Immutable source and workflow

- Tag: `wechat-article-v2.0.0` (annotated, not cryptographically signed)
- Source commit: `c43efc90f6cbfbeb15c0b2a4a6f9c2495f652474`
- Workflow run: <https://github.com/vmxmy/wechat-article-exporter/actions/runs/29928560336>
- Release: <https://github.com/vmxmy/wechat-article-exporter/releases/tag/wechat-article-v2.0.0>
- Published at: `2026-07-22T14:32:01Z`

The workflow completed every release metadata, parity, static analysis, fixture, database compatibility, native test, archive metadata, native smoke, and release job successfully.

## Release assets

The published release is neither a draft nor a prerelease and contains exactly:

- five native archives for macOS Intel, macOS Apple Silicon, Linux x86-64, Linux ARM64, and Windows x86-64;
- five target-specific CycloneDX SBOM files;
- `checksums.txt`.

The assets were downloaded again from the published GitHub Release. `sha256sum --check checksums.txt` passed for all ten archives and SBOMs. Every archive contained the binary, `README.md`, `LICENSE`, and `build-info.txt`. The extracted metadata and SBOM properties matched version `2.0.0`, the declared GOOS/GOARCH target, and `CGO_ENABLED=0`. The native macOS Apple Silicon binary reported version `2.0.0`.

The release tag resolves to the source commit above. It is an annotated Git tag, but no GPG signature is claimed.

## Compatibility services retained

After release publication:

- `https://mp.ziikoo.app/` returned HTTP 200;
- `https://mptext.ziikoo.app/health` returned service status `ok` and `remoteOAuth: compatibility-window`.

This establishes the compatibility window required before the final Web-capable archive and retirement operations.
