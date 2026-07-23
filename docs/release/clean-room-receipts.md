# Clean-room release receipts

Task 17.9 is satisfied only by candidate-binary evidence from all five supported native targets:

- `darwin/arm64`
- `darwin/amd64`
- `linux/arm64`
- `linux/amd64`
- `windows/amd64`

The repository fixture runner is deliberately incapable of producing live evidence. Its `run` command accepts only `--mode fixture`; `--mode live` and `--require-live` fail before any workflow starts. The four account-dependent workflows—QR login, restart persistence, account synchronization, and article/resource download—must be collected by a separately authorized controlled-account runner and validated with `--require-live`.

## Fixture receipt

Extract the candidate archive and point the runner at the extracted binary and the archive's `build-info.txt`:

```bash
cd cli
go run ./cmd/cleanroom run \
  --archive /path/to/wechat-article_VERSION_GOOS_GOARCH.tar.gz \
  --binary /path/to/extracted/wechat-article \
  --build-info /path/to/extracted/build-info.txt \
  --output /path/to/fixture-receipt.json \
  --commit COMMIT_SHA \
  --version VERSION \
  --repository OWNER/REPOSITORY

go run ./cmd/cleanroom verify \
  --receipt /path/to/fixture-receipt.json
```

The runner invokes the extracted candidate binary for every product workflow, including a native-PTY Bubble Tea session and stdio MCP. It uses a loopback WeChat fixture, a persistent encrypted vault, an independent restore root, and a locally installed Chromium-family browser for PDF. Fixture receipts are continuous-integration evidence only and cannot pass the stable live gate.

## Stable platform and aggregate receipts

A stable platform receipt uses schema `wechat-article-clean-room-platform/v1`, has `mode=live`, and binds every workflow executor to the archive's extracted binary SHA-256. It must contain exactly the 24 workflow IDs specified by OpenSpec, with no failures or skips. Published evidence must not include QR data, sessions, account/article identifiers, article bodies, absolute user paths, or raw command output.

After collecting one passing live platform receipt for every supported target, create a `wechat-article-clean-room-release-set/v1` document that references each receipt by relative path and SHA-256. Verify it with:

```bash
cd cli
go run ./cmd/cleanroom verify-set \
  --receipt-set /path/to/release-receipt-set.json
```

The aggregate validator rejects missing or duplicate targets, fixture receipts, different release identities, archive mismatches, invalid platform receipts, and inconsistent summaries.

Do not commit controlled-account credentials, QR images, platform keyring contents, portable roots, databases, exports, backups, or raw network captures. Only redacted receipts may be retained as release evidence.
