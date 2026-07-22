package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestSaveQueryExportAndImportAccountsPreserveRicherLocalMetadata(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	local, err := database.SaveAccount(ctx, domain.Account{
		FakeID: "fixture-a", Name: "Rich Local Name", Alias: "local-alias", Description: "local description",
		AvatarURL: "https://mmbiz.qpic.cn/local/0", ServiceType: 2, ArticleCount: 12,
		LastSyncAt: time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest := domain.AccountManifest{SchemaVersion: AccountManifestVersion, Accounts: []domain.Account{
		{FakeID: "fixture-a", Name: "Sparse Import", ArticleCount: 2},
		{FakeID: "fixture-b", Name: "Imported Account", Alias: "imported"},
	}}
	report, err := database.ImportAccounts(ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if report.Added != 1 || report.Unchanged != 1 || report.Merged != 0 {
		t.Fatalf("report = %#v", report)
	}
	got, err := database.GetAccountByFakeID(ctx, "fixture-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != local.ID || got.Name != "Rich Local Name" || got.Alias != "local-alias" || got.ArticleCount != 12 || got.AvatarURL == "" {
		t.Fatalf("local account was degraded: %#v", got)
	}
	page, err := database.QueryAccounts(ctx, domain.AccountQuery{Keyword: "imported", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].FakeID != "fixture-b" || page.Items[0].Alias != "imported" {
		t.Fatalf("page = %#v", page)
	}
	exported, err := database.ExportAccounts(ctx, domain.AccountQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if exported.SchemaVersion != AccountManifestVersion || len(exported.Accounts) != 2 {
		t.Fatalf("exported = %#v", exported)
	}
}

func TestImportAccountsFillsMissingLocalFieldsWithoutReplacingRicherValues(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	if _, err := database.SaveAccount(ctx, domain.Account{FakeID: "fixture-a", Name: "Local Name"}); err != nil {
		t.Fatal(err)
	}
	report, err := database.ImportAccounts(ctx, domain.AccountManifest{SchemaVersion: AccountManifestVersion, Accounts: []domain.Account{{
		FakeID: "fixture-a", Name: "Incoming Name", Alias: "filled-alias", Description: "filled description",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Merged != 1 {
		t.Fatalf("report = %#v", report)
	}
	got, err := database.GetAccountByFakeID(ctx, "fixture-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Local Name" || got.Alias != "filled-alias" || got.Description != "filled description" {
		t.Fatalf("account = %#v", got)
	}
}

func TestSaveAccountMergesWithoutOverwritingRicherLocalMetadata(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	local, err := database.SaveAccount(ctx, domain.Account{
		FakeID: "fixture-a", Name: "Local Name", Alias: "local-alias", Description: "local description",
		AvatarURL: "https://mmbiz.qpic.cn/local/0", ServiceType: 2, ArticleCount: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := database.SaveAccount(ctx, domain.Account{
		FakeID: "fixture-a", Name: "Upstream Name", Alias: "upstream-alias", Description: "upstream description",
		AvatarURL: "https://mmbiz.qpic.cn/upstream/0", ServiceType: 1, ArticleCount: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID != local.ID || merged.Name != "Local Name" || merged.Alias != "local-alias" ||
		merged.Description != "local description" || merged.AvatarURL != "https://mmbiz.qpic.cn/local/0" ||
		merged.ServiceType != 2 || merged.ArticleCount != 7 {
		t.Fatalf("merged account = %#v", merged)
	}
}

func TestUpdateAccountReplacesEditableMetadataAndPreservesSyncState(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	syncedAt := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	account, err := database.SaveAccount(ctx, domain.Account{
		FakeID: "fixture-a", Name: "Before", Alias: "before", ArticleCount: 8, LastSyncAt: syncedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := database.UpdateAccount(ctx, domain.Account{
		ID: account.ID, FakeID: account.FakeID, Name: "After", Alias: "after", Description: "edited",
		AvatarURL: "https://mmbiz.qpic.cn/edited/0", ServiceType: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "After" || updated.Alias != "after" || updated.Description != "edited" ||
		updated.ArticleCount != 8 || !updated.LastSyncAt.Equal(syncedAt) {
		t.Fatalf("updated account = %#v", updated)
	}
	if _, err := database.UpdateAccount(ctx, domain.Account{ID: account.ID, FakeID: "different", Name: "No"}); err == nil {
		t.Fatal("UpdateAccount(changed fakeid) error = nil")
	}
}

func TestExportAccountsHonorsFilterAcrossPagination(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	for index := 0; index < 520; index++ {
		name := "Other"
		description := "not selected"
		if index%2 == 0 {
			name = "Selected"
			description = "export-scope"
		}
		if _, err := database.SaveAccount(ctx, domain.Account{
			FakeID: fmt.Sprintf("fixture-%03d", index), Name: name, Description: description,
		}); err != nil {
			t.Fatal(err)
		}
	}
	exported, err := database.ExportAccounts(ctx, domain.AccountQuery{Keyword: "export-scope"})
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.Accounts) != 260 {
		t.Fatalf("exported account count = %d", len(exported.Accounts))
	}
}

func TestImportAccountsValidatesEntireManifestBeforeWriting(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	_, err := database.ImportAccounts(context.Background(), domain.AccountManifest{
		SchemaVersion: AccountManifestVersion,
		Accounts:      []domain.Account{{FakeID: "valid", Name: "Valid"}, {FakeID: "", Name: "Invalid"}},
	})
	if err == nil {
		t.Fatal("ImportAccounts() error = nil")
	}
	var count int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE profile_id='profile-a'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("partial account writes = %d", count)
	}
}

func TestDeleteAccountsIsTransactionalAndLeavesObjectsForGarbageCollection(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	account, err := database.SaveAccount(ctx, domain.Account{FakeID: "fixture-a", Name: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sharedDigest := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	sharedAccount, err := database.SaveAccount(ctx, domain.Account{FakeID: "fixture-b", Name: "Shared Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO articles(id, profile_id, account_id, aid, canonical_url, created_at, updated_at)
VALUES('article-a', 'profile-a', ?, 'aid-a', 'https://mp.weixin.qq.com/s/a', ?, ?)`, account.ID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO articles(id, profile_id, account_id, aid, canonical_url, created_at, updated_at)
VALUES('article-b', 'profile-a', ?, 'aid-b', 'https://mp.weixin.qq.com/s/b', ?, ?)`, sharedAccount.ID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec("INSERT INTO objects(digest, size_bytes, created_at) VALUES(?, 10, ?)", digest, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO content_versions(id, article_id, object_digest, kind, captured_at)
VALUES('content-a', 'article-a', ?, 'html', ?)`, digest, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec("INSERT INTO objects(digest, size_bytes, created_at) VALUES(?, 12, ?)", sharedDigest, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO resources(id, profile_id, source_url, object_digest, status, created_at, updated_at)
VALUES('resource-shared', 'profile-a', 'https://mmbiz.qpic.cn/shared/0', ?, 'available', ?, ?);
INSERT INTO article_resources(article_id, resource_id, role, ordinal) VALUES('article-a', 'resource-shared', 'image', 0)`,
		sharedDigest, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO article_resources(article_id, resource_id, role, ordinal)
VALUES('article-b', 'resource-shared', 'image', 0)`); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM objects WHERE digest=?", digest).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != 1 {
		t.Fatalf("object insertion count = %d", before)
	}
	report, err := database.DeleteAccounts(ctx, []domain.AccountID{account.ID})
	if err != nil {
		t.Fatal(err)
	}
	if report.AccountsDeleted != 1 || report.ArticlesDeleted != 1 || report.ObjectsGarbageEligible != 1 {
		t.Fatalf("report = %#v", report)
	}
	var objectsCount, articleCount, resourceCount int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM objects WHERE digest=?", digest).Scan(&objectsCount); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRow("SELECT COUNT(*) FROM articles WHERE id='article-a'").Scan(&articleCount); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRow("SELECT COUNT(*) FROM resources WHERE id='resource-shared'").Scan(&resourceCount); err != nil {
		t.Fatal(err)
	}
	if objectsCount != 1 || articleCount != 0 || resourceCount != 1 {
		t.Fatalf("objects=%d articles=%d resources=%d", objectsCount, articleCount, resourceCount)
	}
	if err := database.db.QueryRow("SELECT COUNT(*) FROM objects WHERE digest=?", sharedDigest).Scan(&objectsCount); err != nil {
		t.Fatal(err)
	}
	if objectsCount != 1 {
		t.Fatalf("shared object was deleted: count=%d", objectsCount)
	}
	if _, err := database.GetAccount(ctx, account.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted account error = %v", err)
	}
}
