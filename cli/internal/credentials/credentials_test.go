package credentials

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

func TestParsersProduceValidatedCredentialRecords(t *testing.T) {
	jsonRecord, err := ParseJSON(strings.NewReader(`{
  "nickname":"Fixture",
  "biz":"fixture-biz",
  "uin":"10001",
  "key":"fixture-key",
  "pass_ticket":"fixture-ticket",
  "wap_sid2":"fixture-sid",
  "appmsg_token":"fixture-token",
  "cookie":"fixture_cookie=one"
}`))
	if err != nil {
		t.Fatal(err)
	}
	environment, err := ParseEnvironment(func(name string) string {
		return map[string]string{
			EnvBiz: "fixture-biz", EnvUIN: "10001", EnvKey: "fixture-key", EnvPassTicket: "fixture-ticket",
			EnvWapSID2: "fixture-sid", EnvAppMsgToken: "fixture-token", EnvCookie: "fixture_cookie=one",
		}[name]
	})
	if err != nil {
		t.Fatal(err)
	}
	interactive, err := ParseInteractive(InteractiveInput{
		Biz: "fixture-biz", UIN: "10001", Key: "fixture-key", PassTicket: "fixture-ticket",
		WapSID2: "fixture-sid", AppMsgToken: "fixture-token", Cookie: "fixture_cookie=one",
	})
	if err != nil {
		t.Fatal(err)
	}
	stdinRecord, err := ParseStdin(bytes.NewBufferString(`{"biz":"fixture-biz","uin":"10001","key":"fixture-key","pass_ticket":"fixture-ticket","wap_sid2":"fixture-sid","appmsg_token":"fixture-token"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []Record{jsonRecord, environment, interactive, stdinRecord} {
		if record.Biz != "fixture-biz" || record.UIN != "10001" || record.Key == "" || record.PassTicket == "" {
			t.Fatalf("record = %#v", record)
		}
	}
}

func TestCredentialParsingRejectsMissingAndOversizedFields(t *testing.T) {
	_, err := ParseJSON(strings.NewReader(`{"biz":"fixture","uin":"1"}`))
	if err == nil || !strings.Contains(err.Error(), "key") {
		t.Fatalf("missing-field error = %v", err)
	}
	_, err = ParseInteractive(InteractiveInput{
		Biz: "fixture", UIN: "1", Key: strings.Repeat("x", MaximumSecretFieldBytes+1),
		PassTicket: "ticket", WapSID2: "sid", AppMsgToken: "token",
	})
	if err == nil || !strings.Contains(err.Error(), "supported limit") {
		t.Fatalf("oversized-field error = %v", err)
	}
	_, err = ParseJSON(strings.NewReader(`{"biz":"fixture","uin":"1","key":"key","pass_ticket":"ticket","wap_sid2":"sid","appmsg_token":"token","unexpected":true}`))
	if err == nil {
		t.Fatal("unknown JSON field was accepted")
	}
}

func TestRecordJSONNeverExposesSecrets(t *testing.T) {
	record := fixtureRecord()
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{record.UIN, record.Key, record.PassTicket, record.WapSID2, record.AppMsgToken, record.Cookie} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("public JSON leaked %q: %s", secret, encoded)
		}
	}
}

func TestServiceRestoresSecretWhenMetadataRemovalFails(t *testing.T) {
	repository := newMemoryRepository()
	secretStore := secrets.NewMemoryStore()
	service := NewService(ServiceOptions{
		Profile: "profile-a", Repository: repository, Accounts: fixedAccounts{account: domain.Account{ID: "account-a", FakeID: "fixture-biz", Name: "Fixture"}},
		Secrets: secretStore,
	})
	metadata, err := service.Import(context.Background(), fixtureRecord())
	if err != nil {
		t.Fatal(err)
	}
	repository.removeErr = errors.New("database unavailable")
	if err := service.Remove(context.Background(), metadata.ID); err == nil {
		t.Fatal("Remove() error = nil")
	}
	if _, err := secretStore.Get(context.Background(), secretRef("profile-a", metadata.SecretRef)); err != nil {
		t.Fatalf("secret was not restored after metadata failure: %v", err)
	}
}

func TestServiceImportsValidatesAssociatesAndRemovesSecurely(t *testing.T) {
	repository := newMemoryRepository()
	secretStore := secrets.NewMemoryStore()
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	service := NewService(ServiceOptions{
		Profile: "profile-a", Repository: repository, Accounts: fixedAccounts{account: domain.Account{ID: "account-a", FakeID: "fixture-biz", Name: "Fixture"}},
		Secrets: secretStore, Validator: ValidatorFunc(func(_ context.Context, record Record) error {
			if record.Key != "fixture-key" {
				t.Fatalf("validator record = %#v", record)
			}
			return nil
		}), Now: func() time.Time { return now },
	})

	metadata, err := service.Import(context.Background(), fixtureRecord())
	if err != nil {
		t.Fatal(err)
	}
	if metadata.AccountID != "account-a" || metadata.SecretRef == "" || metadata.Status != StatusUnknown {
		t.Fatalf("metadata = %#v", metadata)
	}
	if repository.secretValuesContain("fixture-key") {
		t.Fatal("repository metadata contains secret material")
	}
	loadedMetadata, loaded, err := service.LoadForAccount(context.Background(), "account-a")
	if err != nil || loaded.Key != "fixture-key" || loadedMetadata.ID != metadata.ID {
		t.Fatalf("loaded metadata=%#v record=%#v err=%v", loadedMetadata, loaded, err)
	}
	validated, err := service.Validate(context.Background(), metadata.ID)
	if err != nil || validated.Status != StatusValid || !validated.ValidatedAt.Equal(now) {
		t.Fatalf("validated=%#v err=%v", validated, err)
	}
	if err := service.Remove(context.Background(), metadata.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.LoadForAccount(context.Background(), "account-a"); !errors.Is(err, ErrCredentialMissing) {
		t.Fatalf("load after remove error = %v", err)
	}
	if _, err := secretStore.Get(context.Background(), secretRef("profile-a", metadata.SecretRef)); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("secret after remove error = %v", err)
	}
}

func TestServiceRejectsUnknownAccountWithoutPersistingSecret(t *testing.T) {
	repository := newMemoryRepository()
	secretStore := secrets.NewMemoryStore()
	service := NewService(ServiceOptions{
		Profile: "profile-a", Repository: repository, Accounts: fixedAccounts{err: sql.ErrNoRows}, Secrets: secretStore,
	})
	_, err := service.Import(context.Background(), fixtureRecord())
	if err == nil || !strings.Contains(err.Error(), "associate") {
		t.Fatalf("import error = %v", err)
	}
	if len(repository.values) != 0 {
		t.Fatalf("repository values = %#v", repository.values)
	}
}

func TestServiceMarksFailedValidationInvalid(t *testing.T) {
	repository := newMemoryRepository()
	service := NewService(ServiceOptions{
		Profile: "profile-a", Repository: repository, Accounts: fixedAccounts{account: domain.Account{ID: "account-a", FakeID: "fixture-biz", Name: "Fixture"}},
		Secrets: secrets.NewMemoryStore(), Validator: ValidatorFunc(func(context.Context, Record) error { return ErrCredentialExpired }),
	})
	metadata, err := service.Import(context.Background(), fixtureRecord())
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.Validate(context.Background(), metadata.ID)
	if !errors.Is(err, ErrCredentialExpired) || updated.Status != StatusInvalid || updated.ValidatedAt.IsZero() {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
}

func fixtureRecord() Record {
	return Record{
		Nickname: "Fixture", Biz: "fixture-biz", UIN: "10001", Key: "fixture-key",
		PassTicket: "fixture-ticket", WapSID2: "fixture-sid", AppMsgToken: "fixture-token",
		Cookie: "fixture_cookie=one",
	}
}

type fixedAccounts struct {
	account domain.Account
	err     error
}

func (accounts fixedAccounts) GetAccountByFakeID(context.Context, string) (domain.Account, error) {
	return accounts.account, accounts.err
}

type memoryRepository struct {
	values    map[string]Metadata
	removeErr error
}

func newMemoryRepository() *memoryRepository { return &memoryRepository{values: map[string]Metadata{}} }

func (repository *memoryRepository) UpsertCredential(_ context.Context, metadata Metadata) (Metadata, error) {
	repository.values[metadata.ID] = metadata
	return metadata, nil
}

func (repository *memoryRepository) CredentialByID(_ context.Context, id string) (Metadata, error) {
	value, ok := repository.values[id]
	if !ok {
		return Metadata{}, sql.ErrNoRows
	}
	return value, nil
}

func (repository *memoryRepository) CredentialForAccount(_ context.Context, accountID domain.AccountID, kind string) (Metadata, error) {
	for _, value := range repository.values {
		if value.AccountID == accountID && value.Kind == kind {
			return value, nil
		}
	}
	return Metadata{}, sql.ErrNoRows
}

func (repository *memoryRepository) ListCredentials(context.Context) ([]Metadata, error) {
	values := make([]Metadata, 0, len(repository.values))
	for _, value := range repository.values {
		values = append(values, value)
	}
	return values, nil
}

func (repository *memoryRepository) UpdateCredentialStatus(_ context.Context, id string, status Status, validatedAt time.Time) (Metadata, error) {
	value, ok := repository.values[id]
	if !ok {
		return Metadata{}, sql.ErrNoRows
	}
	value.Status = status
	value.ValidatedAt = validatedAt
	repository.values[id] = value
	return value, nil
}

func (repository *memoryRepository) RemoveCredential(_ context.Context, id string) error {
	if repository.removeErr != nil {
		return repository.removeErr
	}
	if _, ok := repository.values[id]; !ok {
		return sql.ErrNoRows
	}
	delete(repository.values, id)
	return nil
}

func (repository *memoryRepository) secretValuesContain(value string) bool {
	encoded, _ := json.Marshal(repository.values)
	return strings.Contains(string(encoded), value)
}
