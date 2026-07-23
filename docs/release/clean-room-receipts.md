# Clean-room release receipts

Task 17.9 is satisfied only by candidate-binary evidence from all five supported native targets:

- `darwin/arm64`
- `darwin/amd64`
- `linux/arm64`
- `linux/amd64`
- `windows/amd64`

The repository fixture runner is deliberately incapable of producing live evidence. Its `run` command accepts only `--mode fixture`; `--mode live` and `--require-live` fail before any workflow starts. The five account-dependent workflows—QR login, restart persistence, account synchronization, article download, and resource download—must be collected by a separately authorized controlled-account runner and validated with `--require-live`.

## Fixture receipt

Provide the candidate archive, its per-target SBOM, and the release checksum manifest. The runner extracts the archive into its own clean root, verifies the archive layout and target metadata, checks both the archive and SBOM against `checksums.txt`, and executes the extracted candidate binary. Optional `--binary` and `--build-info` inputs are accepted only when their digests match the corresponding archive members.

```bash
cd cli
go run ./cmd/cleanroom run \
  --archive /path/to/wechat-article_VERSION_GOOS_GOARCH.tar.gz \
  --sbom /path/to/wechat-article_VERSION_GOOS_GOARCH.sbom.cdx.json \
  --checksums /path/to/checksums.txt \
  --output /path/to/fixture-receipt.json \
  --commit COMMIT_SHA \
  --version VERSION \
  --repository OWNER/REPOSITORY

go run ./cmd/cleanroom verify \
  --receipt /path/to/fixture-receipt.json
```

The runner invokes the extracted candidate binary for every product workflow, including a native-PTY Bubble Tea session and stdio MCP. It uses a loopback WeChat fixture, a persistent encrypted vault, an independent restore root, and a locally installed Chromium-family browser for PDF. Fixture receipts are continuous-integration evidence only and cannot pass the stable live gate. Closing the loopback fixture is diagnostic offline coverage; it is not the operating-system deny-all proof required from the controlled live runner.

## Stable platform and aggregate receipts

A stable platform receipt uses schema `wechat-article-clean-room-platform/v1`, has `mode=live`, and binds every workflow executor to the archive's extracted binary SHA-256. It must contain exactly the 24 workflow IDs specified by OpenSpec, with no failures or skips. The validator accepts only the workflow-specific evidence keys and bounded value forms registered by the schema. Published evidence must not include QR data, sessions, account/article identifiers, article bodies, absolute user paths, or raw command output.

After collecting one passing live platform receipt for every supported target, create a `wechat-article-clean-room-release-set/v1` document that references each receipt by relative path and SHA-256. Verify it with:

```bash
cd cli
go run ./cmd/cleanroom verify-set \
  --receipt-set /path/to/release-receipt-set.json \
  --repository OWNER/REPOSITORY \
  --tag wechat-article-vVERSION \
  --commit COMMIT_SHA \
  --version VERSION \
  --checksum-manifest-sha256 CHECKSUMS_SHA256
```

The aggregate validator rejects missing or duplicate targets, fixture receipts, different release identities, archive mismatches, invalid platform receipts, and inconsistent summaries.

Stable publication also fails closed unless the repository variables `CONTROLLED_LIVE_RECEIPT_RUN_ID` and `CONTROLLED_LIVE_WORKFLOW_ID` identify a successful, explicitly dispatched run of the approved controlled-live workflow for the same repository and source commit. The release workflow verifies the run's repository, workflow identity, event, conclusion, and commit provenance, and requires the aggregate document's repository, tag, version, commit, and checksum-manifest digest to match the current tagged release before any assets are published.

Do not commit controlled-account credentials, QR images, platform keyring contents, portable roots, databases, exports, backups, or raw network captures. Only redacted receipts may be retained as release evidence.
