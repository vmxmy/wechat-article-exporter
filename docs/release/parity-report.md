# 16.7 Mandatory Parity Audit

- Audit date: 2026-07-22
- Change: `replace-web-with-local-go-cli`
- Sign-off: **signed-off** — All 24 mandatory parity entries have implementation, fixture or controlled-account evidence. This signs off task 16.7 only; compatibility release publication, final Web-capable archive, operational cloud shutdown, and clean-room retirement validation remain separate gates.
- Release gate: **passed** (24/24 mandatory entries passed)
- Gate execution: **executed**. The executable mandatory parity gate passed.
- Web/Nitro/remote MCP code remains in place until the compatibility release and final Web-capable archive requirements are also satisfied.

This report is generated from `test/parity/matrix.json`. `yarn test:parity` verifies the matrix, every referenced test/fixture, `test/parity/report.json`, and this Markdown file are mutually consistent.

## Executed verification

- `cd cli && go test ./...`: **passed**
- `cd cli && go test -race ./...`: **passed**
- `cd cli && go test ./internal/exporter -run 'TestRealChromiumRendersCuratedSelfContainedPDF|TestGeneratedDOCXOpensInLibreOfficeHeadless' -count=1 -v`: **passed** — Real Google Chrome PDF rendering and LibreOffice DOCX open/conversion both passed on macOS.
- `cd cli && go test ./internal/app -run 'TestCLIWorkspaceRealPTYNavigationResizeAndCleanExit' -count=1 -v`: **passed**
- `controlled-account local QR smoke: login -> restart/status -> logout`: **passed** — On 2026-07-22 an isolated portable profile exercised the live WeChat flow through waiting, scanned, and confirmed states; a new CLI process revalidated the persisted OS-keyring session as authenticated/valid; public JSON contained no token or cookies; logout removed the session manifest, chunks, and profile index. No live credential, QR image, account identifier, or article data is committed as evidence.
- `GitHub Actions native-terminal-verification run 29914112307 (ubuntu-latest, macos-15-intel, windows-latest)`: **passed** — The real pseudo-terminal navigation, resize, and clean-exit smoke passed on all three native runners: https://github.com/vmxmy/wechat-article-exporter/actions/runs/29914112307
- `cd cli && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 -checks='SA*,S1*,QF*' ./...`: **passed**
- `CGO_ENABLED=0 GOOS/GOARCH cross-build matrix: darwin amd64/arm64, linux amd64/arm64, windows amd64/arm64`: **passed**
- `yarn test:baseline && yarn test:api-core && ./node_modules/.bin/vite-node -c test/vite.config.ts test/legacy-export/archive.test.ts`: **passed**
- `yarn build`: **passed**
- `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/release-cli.yml`: **passed**
- `openspec validate replace-web-with-local-go-cli --strict && git diff --check`: **passed**
- `yarn test:parity`: **passed**
- `yarn test:parity:gate`: **passed** — All 24 mandatory parity entries passed after the controlled-account QR smoke and cross-platform terminal verification.

## Mandatory matrix

| ID | Status | Test evidence | Fixtures | Blocker |
| --- | --- | ---: | ---: | --- |
| `auth.qr-login` | passed | 9 | 0 | — |
| `auth.session-lifecycle` | passed | 5 | 0 | — |
| `accounts.search-resolve` | passed | 4 | 4 | — |
| `accounts.library` | passed | 5 | 0 | — |
| `articles.sync` | passed | 6 | 3 | — |
| `articles.sync-range` | passed | 3 | 0 | — |
| `articles.query-filter` | passed | 3 | 0 | — |
| `articles.single` | passed | 5 | 1 | — |
| `albums.traverse` | passed | 5 | 3 | — |
| `download.article-html` | passed | 6 | 4 | — |
| `download.resources` | passed | 5 | 1 | — |
| `download.metrics` | passed | 5 | 2 | — |
| `download.comments` | passed | 5 | 3 | — |
| `processing.article-types` | passed | 5 | 10 | — |
| `export.html` | passed | 5 | 2 | — |
| `export.text-markdown-json` | passed | 6 | 2 | — |
| `export.excel` | passed | 4 | 1 | — |
| `export.docx` | passed | 4 | 1 | — |
| `export.pdf` | passed | 6 | 2 | — |
| `storage.local-library` | passed | 7 | 0 | — |
| `settings.preferences` | passed | 8 | 0 | — |
| `automation.cobra` | passed | 9 | 0 | — |
| `ui.terminal-workspace` | passed | 10 | 0 | — |
| `automation.local-mcp` | passed | 7 | 0 | — |

## Reproduction evidence

### auth.qr-login — passed

- Command: `cd cli && go test ./internal/wechat ./internal/secrets ./internal/app`
- Command: `controlled-account local QR smoke: login -> restart/status -> logout`
- Test: `cli/internal/wechat/wechat_test.go#TestBeginLoginCapturesUUIDAndReturnsUpstreamQRImage`
- Test: `cli/internal/wechat/wechat_test.go#TestBeginLoginAcceptsCurrentUpstreamJPEGQRImage`
- Test: `cli/internal/wechat/wechat_test.go#TestBeginLoginRestoresUUIDFromCurrentJSONResponse`
- Test: `cli/internal/wechat/wechat_test.go#TestPollLoginStatusFixtures`
- Test: `cli/internal/wechat/wechat_test.go#TestLoginRefreshesExpiredQRWithinBound`
- Test: `cli/internal/wechat/wechat_test.go#TestLoginCancellationStopsPollingWithoutCompletingOrPersistingSession`
- Test: `cli/internal/secrets/keyring_test.go#TestKeyringStoreChunksLargeSecretsAndReassemblesThem`
- Test: `cli/internal/secrets/keyring_test.go#TestKeyringStoreDeletesChunkedSecretsAcrossProcessRestart`
- Test: `cli/internal/app/app_test.go#TestLocalLoginRequiresQROutputWhenNonInteractive`

### auth.session-lifecycle — passed

- Command: `cd cli && go test ./internal/wechat ./internal/secrets ./internal/app`
- Test: `cli/internal/wechat/wechat_test.go#TestCompleteLoginPersistsTokenCookieAttributesAndIdentity`
- Test: `cli/internal/wechat/wechat_test.go#TestPersistedSessionSurvivesClientAndProcessRuntimeRecreation`
- Test: `cli/internal/wechat/wechat_test.go#TestSessionStatusExpiredAndNetworkUnknown`
- Test: `cli/internal/wechat/wechat_test.go#TestAccountSwitchFixture`
- Test: `cli/internal/wechat/wechat_test.go#TestLogoutDeletesLocalSecretWhenUpstreamFails`

### accounts.search-resolve — passed

- Command: `cd cli && go test ./internal/wechat ./internal/application`
- Test: `cli/internal/wechat/discovery_test.go#TestSearchAccountsUsesFixturePaginationAndNormalizes`
- Test: `cli/internal/wechat/discovery_test.go#TestResolveAccountNameRejectsUnsupportedURLBeforeRequest`
- Test: `cli/internal/wechat/discovery_test.go#TestResolveAccountNameRejectsRedirectOutsideAllowedHosts`
- Test: `cli/internal/application/application_test.go#TestApplicationDiscoveryMethodsUseSharedGateway`
- Fixture: `cli/internal/wechat/testdata/discovery/search-success.json`
- Fixture: `cli/internal/wechat/testdata/discovery/search-empty.json`
- Fixture: `cli/internal/wechat/testdata/discovery/article-account.html`
- Fixture: `cli/internal/wechat/testdata/discovery/author-success.json`

### accounts.library — passed

- Command: `cd cli && go test ./internal/library ./internal/application`
- Test: `cli/internal/library/accounts_test.go#TestSaveQueryExportAndImportAccountsPreserveRicherLocalMetadata`
- Test: `cli/internal/library/accounts_test.go#TestSaveAccountMergesWithoutOverwritingRicherLocalMetadata`
- Test: `cli/internal/library/accounts_test.go#TestImportAccountsValidatesEntireManifestBeforeWriting`
- Test: `cli/internal/library/accounts_test.go#TestDeleteAccountsIsTransactionalAndLeavesObjectsForGarbageCollection`
- Test: `cli/internal/application/application_test.go#TestApplicationAccountOperationsUseSharedLibrary`

### articles.sync — passed

- Command: `cd cli && go test ./internal/wechat ./internal/sync ./internal/library`
- Test: `cli/internal/wechat/discovery_test.go#TestBuildAndParseArticleListFixtures`
- Test: `cli/internal/sync/sync_test.go#TestRunnerCommitsPagesPacesAndStopsAtDateBoundary`
- Test: `cli/internal/sync/sync_test.go#TestRunnerResumesFromCheckpointAndBlocksOnAuthentication`
- Test: `cli/internal/library/articles_test.go#TestSaveArticlePageTracksPaginationCompletionAndAllowsTotalCorrection`
- Test: `cli/internal/library/articles_test.go#TestSaveArticlePageDeduplicatesRepeatedPagesAndPersistsFinalSyncTime`
- Test: `cli/internal/app/local_commands_test.go#TestLocalSyncRuntimeCreatesOneDurableMultiAccountJobWithIndependentItems`
- Fixture: `cli/internal/wechat/testdata/discovery/article-page-one.json`
- Fixture: `cli/internal/wechat/testdata/discovery/article-page-top-level-total.json`
- Fixture: `cli/internal/wechat/testdata/discovery/search-auth-expired.json`

### articles.sync-range — passed

- Command: `cd cli && go test ./internal/sync ./internal/app`
- Test: `cli/internal/sync/sync_test.go#TestResolveBoundarySupportsCurrentRangesAndIncrementalState`
- Test: `cli/internal/sync/sync_test.go#TestRunnerCommitsPagesPacesAndStopsAtDateBoundary`
- Test: `cli/internal/sync/sync_test.go#TestValidatePacingWarnsAndRequiresConfirmationForPersistentUnsafeValue`

### articles.query-filter — passed

- Command: `cd cli && go test ./internal/library`
- Test: `cli/internal/library/article_query_test.go#TestQueryArticlesAppliesAllCompoundFiltersLocally`
- Test: `cli/internal/library/article_query_test.go#TestQueryArticlesStableSortAndPaginationUseIDTieBreak`
- Test: `cli/internal/library/article_query_test.go#TestQueryArticlesRejectsUntrustedSortSQL`

### articles.single — passed

- Command: `cd cli && go test ./internal/library ./internal/tui`
- Test: `cli/internal/library/single_test.go#TestNormalizeArticleURLRemovesTrackingFragmentsAndCanonicalizesHost`
- Test: `cli/internal/library/single_test.go#TestProvisionalSingleArticleDeduplicatesAndRepairsRealFakeID`
- Test: `cli/internal/library/single_test.go#TestRepairSingleArticleMergesCollidingContentCommentsRepliesAndCheckpoint`
- Test: `cli/internal/app/local_commands_test.go#TestSingleArticleDownloadRepairsProvisionalIdentityAndKeepsContentReadable`
- Test: `cli/internal/tui/workspace_test.go#TestWorkspaceSafePreviewAndExplicitHTMLHandoff`
- Fixture: `samples/普通图文/01.html`

### albums.traverse — passed

- Command: `cd cli && go test ./internal/wechat ./internal/sync ./internal/library ./internal/exporter`
- Test: `cli/internal/wechat/album_test.go#TestListAlbumArticlesNormalizesForwardReverseAndContinuation`
- Test: `cli/internal/wechat/album_test.go#TestTraverseAlbumResumesDeduplicatesAndPersistsCheckpoint`
- Test: `cli/internal/sync/album_test.go#TestAlbumRunnerPersistsStableGlobalOrderAcrossPages`
- Test: `cli/internal/app/local_commands_test.go#TestLocalAlbumRuntimeTraversesPagesThenQueuesOneBatchDownloadJob`
- Test: `cli/internal/exporter/selection_test.go#TestBuildSelectionManifestAcceptsEverySelectionSource`
- Fixture: `cli/internal/wechat/testdata/discovery/album-forward.json`
- Fixture: `cli/internal/wechat/testdata/discovery/album-reverse.json`
- Fixture: `cli/internal/wechat/testdata/discovery/album-continuation.json`

### download.article-html — passed

- Command: `cd cli && go test ./internal/download ./internal/processor ./internal/app`
- Test: `cli/internal/download/download_test.go#TestArticleDownloaderSkipsValidCachedContent`
- Test: `cli/internal/download/download_test.go#TestArticleDownloaderForceRefreshBypassesValidCachedContent`
- Test: `cli/internal/download/e2e_test.go#TestFakeUpstreamArticleCasesCachedSuccessDeletedRestrictedRiskAndProxyFailure`
- Test: `cli/internal/download/download_test.go#TestArticleDownloaderClassifiesAndCapturesRiskControlWithoutValidCommit`
- Test: `cli/internal/download/jobs_test.go#TestPersistentArticleJobClassifiesDeletedRiskAndProxyFailures`
- Test: `cli/internal/app/local_commands_test.go#TestNoWaitDownloadDetachedWorkerCompletesAfterParentReturns`
- Fixture: `samples/普通图文/01.html`
- Fixture: `samples/作者已删除/01.html`
- Fixture: `samples/内容违规/01.html`
- Fixture: `samples/该内容暂时无法查看/01.html`

### download.resources — passed

- Command: `cd cli && go test ./internal/processor ./internal/download ./internal/library ./internal/objects`
- Test: `cli/internal/processor/render_test.go#TestDiscoverResourcesCoversSupportedKindsAndNormalizesURLs`
- Test: `cli/internal/download/download_test.go#TestResourceDownloaderReusesCacheAndRecordsMissing`
- Test: `cli/internal/library/content_test.go#TestCommitResourceDeduplicatesBySourceAndTracksArticleMapping`
- Test: `cli/internal/objects/store_test.go#TestFileStoreStreamsDeduplicatesAndValidates`
- Test: `cli/internal/app/local_commands_test.go#TestResourceDownloadDiscoversStoredArticleResources`
- Fixture: `cli/internal/processor/testdata/sanitized/rich-content.html`

### download.metrics — passed

- Command: `cd cli && go test ./internal/wechat ./internal/download ./internal/library`
- Test: `cli/internal/wechat/content_test.go#TestContentEndpointUsesSensitiveClassesWithoutLeakingArticleCredentialIntoURL`
- Test: `cli/internal/download/metadata_test.go#TestMetadataDownloaderPreflightsCredentialAndPersistsProvenance`
- Test: `cli/internal/download/metadata_test.go#TestMetadataDownloaderRejectsMissingCredentialBeforeNetwork`
- Test: `cli/internal/download/metadata_test.go#TestMetadataDownloaderMarksExpiredCredentialInvalid`
- Test: `cli/internal/library/metrics_test.go#TestMetricSnapshotsPersistCaptureTimeAndCredentialProvenance`
- Fixture: `cli/internal/wechat/testdata/content/engagement-success.json`
- Fixture: `cli/internal/wechat/testdata/content/credential-expired.html`

### download.comments — passed

- Command: `cd cli && go test ./internal/wechat ./internal/download ./internal/library`
- Test: `cli/internal/wechat/content_test.go#TestContentEndpointBuildsCommentContinuationAndReplyRequests`
- Test: `cli/internal/download/comments_test.go#TestCommentsDownloaderPagesDeduplicatesAndResumesReplyFailures`
- Test: `cli/internal/download/comments_test.go#TestCommentsDownloaderKeepsContinuationCheckpointOnPageFailure`
- Test: `cli/internal/library/comments_test.go#TestCommitCommentPageDeduplicatesAndPersistsContinuationAtomically`
- Test: `cli/internal/library/replies_test.go#TestReplyThreadPartialFailurePersistsAndResumeTargetsOnlyIncomplete`
- Fixture: `cli/internal/wechat/testdata/content/comments-page-one.json`
- Fixture: `cli/internal/wechat/testdata/content/comments-page-two.json`
- Fixture: `cli/internal/wechat/testdata/content/replies-page.json`

### processing.article-types — passed

- Command: `cd cli && go test ./internal/processor`
- Test: `cli/internal/processor/processor_test.go#TestRepresentativeSamples`
- Test: `cli/internal/processor/processor_test.go#TestRepresentativeUnavailableSamples`
- Test: `cli/internal/processor/render_test.go#TestMessageTypeFixturesRender`
- Test: `cli/internal/processor/render_test.go#TestSampleSemanticAndStructuralGoldenSuite`
- Test: `cli/internal/processor/processor_test.go#TestNoExecutionSentinel`
- Fixture: `samples/普通图文/01.html`
- Fixture: `samples/文本分享/01.html`
- Fixture: `samples/图片分享/01.html`
- Fixture: `samples/文章分享/01.html`
- Fixture: `samples/作者已删除/01.html`
- Fixture: `samples/内容违规/01.html`
- Fixture: `samples/该内容暂时无法查看/01.html`
- Fixture: `cli/internal/processor/testdata/types/audio.html`
- Fixture: `cli/internal/processor/testdata/types/video.html`
- Fixture: `cli/internal/processor/testdata/golden/sample_semantics.json`

### export.html — passed

- Command: `cd cli && go test ./internal/exporter ./internal/processor`
- Test: `cli/internal/exporter/html_test.go#TestExportHTMLArticleWritesSelfContainedResourcesAndComments`
- Test: `cli/internal/exporter/html_test.go#TestExportHTMLArticleStrictMissingResourcePublishesNothing`
- Test: `cli/internal/exporter/html_test.go#TestExportHTMLArticleBestEffortReportsMissingResources`
- Test: `cli/internal/exporter/html_test.go#TestExportHTMLBatchArchiveIsDeterministicAndPortable`
- Test: `cli/internal/exporter/regression_test.go#TestFormatSpecificGoldenAndStructuralRegression`
- Fixture: `cli/internal/exporter/testdata/regression_article.json`
- Fixture: `cli/internal/exporter/testdata/regression_golden.json`

### export.text-markdown-json — passed

- Command: `cd cli && go test ./internal/exporter`
- Test: `cli/internal/exporter/text_test.go#TestRenderTextProducesUTF8WithOptionalMetadataHeader`
- Test: `cli/internal/exporter/markdown_test.go#TestRenderMarkdownFrontMatterAndDefaultHTMLPolicy`
- Test: `cli/internal/exporter/json_test.go#TestMarshalJSONExportIncludesOptionalContentMetricsCommentsRepliesAlbumsAndProvenance`
- Test: `cli/internal/exporter/json_test.go#TestExportJSONFileIsDeterministicForExplicitTimestamp`
- Test: `cli/internal/exporter/naming_test.go#TestRenderFilenameUsesDeterministicTemplatesAcrossPlatforms`
- Test: `cli/internal/exporter/regression_test.go#TestFormatSpecificGoldenAndStructuralRegression`
- Fixture: `cli/internal/exporter/testdata/regression_article.json`
- Fixture: `cli/internal/exporter/testdata/regression_golden.json`

### export.excel — passed

- Command: `cd cli && go test ./internal/exporter`
- Test: `cli/internal/exporter/xlsx_test.go#TestWriteXLSXStreamsStableColumnsAndOptionalContent`
- Test: `cli/internal/exporter/xlsx_test.go#TestWriteXLSXUsesDeterministicPackageOrder`
- Test: `cli/internal/exporter/batch_test.go#TestLargeBatchXLSXMemoryAndThroughput`
- Test: `cli/internal/exporter/regression_test.go#TestFormatSpecificGoldenAndStructuralRegression`
- Fixture: `cli/internal/exporter/testdata/xlsx_columns.json`

### export.docx — passed

- Command: `cd cli && go test ./internal/exporter`
- Command: `cd cli && go test ./internal/exporter -run TestGeneratedDOCXOpensInLibreOfficeHeadless -count=1 -v`
- Test: `cli/internal/exporter/docx_test.go#TestWriteDOCXEmbedsSemanticStructureMediaAndComments`
- Test: `cli/internal/exporter/docx_test.go#TestWriteAndValidateDOCXEnforceBoundsAndCancellation`
- Test: `cli/internal/exporter/docx_test.go#TestGeneratedDOCXOpensInLibreOfficeHeadless`
- Test: `cli/internal/exporter/regression_test.go#TestFormatSpecificGoldenAndStructuralRegression`
- Fixture: `cli/internal/exporter/testdata/docx_article.html`

### export.pdf — passed

- Command: `cd cli && go test ./internal/exporter ./internal/app`
- Command: `cd cli && go test ./internal/exporter -run TestRealChromiumRendersCuratedSelfContainedPDF -count=1 -v`
- Test: `cli/internal/exporter/pdf_test.go#TestDiscoverChromiumUsesDeterministicSupportedCandidates`
- Test: `cli/internal/exporter/pdf_test.go#TestRenderPDFUsesOnlyLocalSelfContainedHTML`
- Test: `cli/internal/exporter/pdf_test.go#TestRenderPDFRejectsRemoteResourcesTimeoutAndCancellation`
- Test: `cli/internal/exporter/pdf_test.go#TestRealChromiumRendersCuratedSelfContainedPDF`
- Test: `cli/internal/exporter/regression_test.go#TestCuratedHTMLPDFVisualAndStructuralRegression`
- Test: `cli/internal/app/local_commands_test.go#TestLocalExportRuntimeExecutesPDFAndPersistsManifestState`
- Fixture: `cli/internal/exporter/testdata/pdf_self_contained.html`
- Fixture: `cli/internal/exporter/testdata/regression_golden.json`

### storage.local-library — passed

- Command: `cd cli && go test ./internal/library ./internal/objects`
- Test: `cli/internal/library/database_test.go#TestWithTxRollsBackAllRelatedChanges`
- Test: `cli/internal/library/database_test.go#TestDatabaseTypedQueriesAreStableAndProfileIsolated`
- Test: `cli/internal/library/database_test.go#TestSQLiteWALSupportsSeparateProcessReaderDuringWriterAndSeesCommittedStateAfterward`
- Test: `cli/internal/library/migrations_test.go#TestMigrationFailureRollsBackVersionAndSchema`
- Test: `cli/internal/library/backup_restore_test.go#TestRestoreValidatesBeforeMutationAndRollsBackBeforeCommit`
- Test: `cli/internal/library/integrity_test.go#TestIntegrityMarksArticleIncompleteWhenSharedObjectIsMissing`
- Test: `cli/internal/library/gc_test.go#TestLibraryGarbageCollectionDryRunConfirmationAndRetention`

### settings.preferences — passed

- Command: `cd cli && go test ./internal/profiles ./internal/network ./internal/sync`
- Test: `cli/internal/profiles/config_test.go#TestConfigStoreWritesAtomicVersionedConfiguration`
- Test: `cli/internal/profiles/config_test.go#TestConfigStoreMigratesVersionZeroWithBackup`
- Test: `cli/internal/profiles/config_test.go#TestConfigStoreRoundTripsAllRetainedPreferences`
- Test: `cli/internal/profiles/config_test.go#TestConfigStoreMigratesVersionOneWithBackupAndPreservesValues`
- Test: `cli/internal/profiles/config_test.go#TestConfigStoreRejectsUnsafePacingWithoutConfirmation`
- Test: `cli/internal/profiles/config_test.go#TestConfigStoreSerializesConcurrentUpdates`
- Test: `cli/internal/sync/sync_test.go#TestValidatePacingWarnsAndRequiresConfirmationForPersistentUnsafeValue`
- Test: `cli/internal/app/local_commands_test.go#TestLocalStatusJSONExposesAllEffectiveRetainedPreferences`

### automation.cobra — passed

- Command: `cd cli && go test ./internal/app ./internal/safety`
- Test: `cli/internal/app/local_commands_test.go#TestLocalCommandGroupsArePresentInHelp`
- Test: `cli/internal/app/local_commands_test.go#TestLocalJSONSuccessEnvelopeIsOnePureDocument`
- Test: `cli/internal/app/local_commands_test.go#TestLocalErrorExitCodesAndVersionedJSON`
- Test: `cli/internal/app/local_commands_test.go#TestAsyncJobStartWaitAndFollowKeepProgressOnStderr`
- Test: `cli/internal/app/local_commands_test.go#TestResumeJobRelaunchesPersistentWorker`
- Test: `cli/internal/app/local_commands_test.go#TestSignalInterruptionProducesDistinctStructuredResult`
- Test: `cli/internal/app/local_commands_test.go#TestDestructiveCommandsRequireExactConfirmation`
- Test: `cli/internal/app/local_commands_test.go#TestCompletionWritesShellScriptWithoutJSONEnvelope`
- Test: `cli/internal/app/app_test.go#TestOutputAppliesCentralRedactionBeforeJSONSerialization`

### ui.terminal-workspace — passed

- Command: `cd cli && go test ./internal/tui ./internal/app`
- Command: `cd cli && go test ./internal/app -run TestCLIWorkspaceRealPTYNavigationResizeAndCleanExit -count=1 -v`
- Command: `GitHub Actions native-terminal-verification run 29914112307 (ubuntu-latest, macos-15-intel, windows-latest)`
- Test: `cli/internal/tui/workspace_test.go#TestWorkspaceInitialLoadAndMajorNavigationUseApplicationSeam`
- Test: `cli/internal/tui/workspace_test.go#TestWorkspaceArticleSelectionStartsOneDownloadJobForStableIDs`
- Test: `cli/internal/tui/workspace_test.go#TestWorkspaceSafePreviewAndExplicitHTMLHandoff`
- Test: `cli/internal/tui/workspace_test.go#TestWorkspaceOnboardingQRAndOfflineEntryPoints`
- Test: `cli/internal/tui/workspace_test.go#TestWorkspaceNoColorUnicodeFallbackStateRoundTripAndNonTTYGuard`
- Test: `cli/internal/tui/workspace_test.go#TestWorkspaceAreasExposeAllOpenSpecWorkflows`
- Test: `cli/internal/tui/workspace_test.go#TestWorkspaceQuerySearchPagesAndColumnSelection`
- Test: `cli/internal/app/local_commands_test.go#TestNoSubcommandRoutesInteractiveStartupToFullWorkspaceWithSavedDisplayPreferences`
- Test: `cli/internal/app/workspace_pty_unix_test.go#TestCLIWorkspaceRealPTYNavigationResizeAndCleanExit`
- Test: `cli/internal/app/workspace_pty_windows_test.go#TestCLIWorkspaceRealPTYNavigationResizeAndCleanExit`

### automation.local-mcp — passed

- Command: `cd cli && go test ./internal/mcp ./internal/app`
- Test: `cli/internal/mcp/adapter_test.go#TestToolSchemasAreStableCompleteAndAnnotated`
- Test: `cli/internal/mcp/adapter_test.go#TestSharedApplicationContractReturnsMatchingQueriesAndPersistentJobs`
- Test: `cli/internal/mcp/adapter_test.go#TestPolicyReadOnlyAllowDenyConfirmationAndSensitiveRestrictions`
- Test: `cli/internal/mcp/adapter_test.go#TestServerFramingIsolationMalformedBoundsAndEOF`
- Test: `cli/internal/mcp/adapter_test.go#TestToolErrorsAreRedactedAndPackageHasNoRemoteOAuthDependency`
- Test: `cli/internal/mcp/adapter_test.go#TestGoSDKClientConformsToLocalJSONRPCServer`
- Test: `cli/internal/app/local_commands_test.go#TestLocalMCPServeUsesStdioAndProfilePolicy`

## Migration-only entries

- `migration.web-export-import`: **passed**
- `migration.remote-cli`: **passed**

## Known intentional differences

- `ui.terminal-workspace`: The terminal workspace preserves workflows and query power rather than reproducing the browser/AG Grid visual layout.
- `automation.local-mcp`: The local MCP server uses stdio under the local OS account and intentionally does not retain remote OAuth or an HTTP transport.
- `migration.remote-cli`: Legacy OAuth tokens are deliberately preserved only for rollback and are never imported as local WeChat credentials.
- `web.proxy-monitoring`: Hosted fleet monitoring and IP-block dashboards are not local-product workflows; configured-route health and diagnostics are the local replacement.
- `web.api-docs`: Browser-only API exploration is replaced by Cobra help, structured JSON contracts, examples, and MCP tool discovery.
- `web.support`: Support, sponsor, and community links are static documentation rather than native application workflows.
- `web.cloud-sync`: Multi-tenant D1 synchronization is intentionally replaced by per-profile local SQLite, backups, and explicit migration archives.
- `dev.pages`: Development-only demonstration and diagnostic pages are not shipped as native product workflows.

## Sign-off decision

Signed off: All 24 mandatory parity entries have implementation, fixture or controlled-account evidence. This signs off task 16.7 only; compatibility release publication, final Web-capable archive, operational cloud shutdown, and clean-room retirement validation remain separate gates.
