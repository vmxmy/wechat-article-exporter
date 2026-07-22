package library

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestCredentialRepositoryStoresOnlyReferencesAndTracksStatus(t *testing.T) {
	database := openContentDatabase(t)
	seedContentArticle(t, database)
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	metadata, err := database.UpsertCredential(context.Background(), credentials.Metadata{
		ID: "credential-a", AccountID: domain.AccountID("account-a"), Kind: credentials.ArticleKind,
		SecretRef: "credential-a", Status: credentials.StatusUnknown, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.SecretRef != "credential-a" || metadata.Status != credentials.StatusUnknown {
		t.Fatalf("metadata = %#v", metadata)
	}
	byAccount, err := database.CredentialForAccount(context.Background(), "account-a", credentials.ArticleKind)
	if err != nil || byAccount.ID != metadata.ID {
		t.Fatalf("by account = %#v, %v", byAccount, err)
	}
	validated, err := database.UpdateCredentialStatus(context.Background(), metadata.ID, credentials.StatusValid, now.Add(time.Minute))
	if err != nil || validated.Status != credentials.StatusValid || validated.ValidatedAt.IsZero() {
		t.Fatalf("validated = %#v, %v", validated, err)
	}
	listed, err := database.ListCredentials(context.Background())
	if err != nil || len(listed) != 1 || listed[0].ID != metadata.ID {
		t.Fatalf("listed = %#v, %v", listed, err)
	}
	if err := database.RemoveCredential(context.Background(), metadata.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CredentialByID(context.Background(), metadata.ID); !errors.Is(err, credentials.ErrCredentialMissing) {
		t.Fatalf("lookup after remove error = %v", err)
	}
}
