package credentials

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

const (
	ArticleKind = "wechat-article"

	MaximumCredentialBytes  = 256 << 10
	MaximumSecretFieldBytes = 64 << 10

	EnvNickname    = "WECHAT_ARTICLE_NICKNAME"
	EnvBiz         = "WECHAT_ARTICLE_BIZ"
	EnvUIN         = "WECHAT_ARTICLE_UIN"
	EnvKey         = "WECHAT_ARTICLE_KEY"
	EnvPassTicket  = "WECHAT_ARTICLE_PASS_TICKET"
	EnvWapSID2     = "WECHAT_ARTICLE_WAP_SID2"
	EnvAppMsgToken = "WECHAT_ARTICLE_APPMSG_TOKEN"
	EnvCookie      = "WECHAT_ARTICLE_COOKIE"
	EnvExpiresAt   = "WECHAT_ARTICLE_EXPIRES_AT"
)

var (
	ErrCredentialMissing = errors.New("credential is missing; import a credential for this account")
	ErrCredentialExpired = errors.New("credential expired; import or validate a fresh credential")
)

type Status string

const (
	StatusUnknown Status = "unknown"
	StatusValid   Status = "valid"
	StatusInvalid Status = "invalid"
)

type Record struct {
	Nickname    string    `json:"nickname,omitempty"`
	Biz         string    `json:"biz"`
	UIN         string    `json:"uin"`
	Key         string    `json:"key"`
	PassTicket  string    `json:"pass_ticket"`
	WapSID2     string    `json:"wap_sid2"`
	AppMsgToken string    `json:"appmsg_token"`
	Cookie      string    `json:"cookie,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
}

type recordSecret Record

func (record Record) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Nickname  string    `json:"nickname,omitempty"`
		Biz       string    `json:"biz"`
		ExpiresAt time.Time `json:"expiresAt,omitempty"`
	}{Nickname: record.Nickname, Biz: record.Biz, ExpiresAt: record.ExpiresAt})
}

func (record Record) marshalSecret() ([]byte, error) { return json.Marshal(recordSecret(record)) }

func unmarshalSecret(value []byte) (Record, error) {
	var secret recordSecret
	if err := json.Unmarshal(value, &secret); err != nil {
		return Record{}, fmt.Errorf("decode credential secret: %w", err)
	}
	return ValidateRecord(Record(secret))
}

type InteractiveInput Record

func ParseInteractive(input InteractiveInput) (Record, error) { return ValidateRecord(Record(input)) }

func ParseStdin(reader io.Reader) (Record, error) { return ParseJSON(reader) }

func ParseJSON(reader io.Reader) (Record, error) {
	if reader == nil {
		return Record{}, errors.New("credential input is required")
	}
	decoder := json.NewDecoder(io.LimitReader(reader, MaximumCredentialBytes+1))
	decoder.DisallowUnknownFields()
	var record recordSecret
	if err := decoder.Decode(&record); err != nil {
		return Record{}, fmt.Errorf("decode credential JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return Record{}, errors.New("credential JSON contains multiple values")
	} else if !errors.Is(err, io.EOF) {
		return Record{}, fmt.Errorf("decode trailing credential JSON: %w", err)
	}
	return ValidateRecord(Record(record))
}

func ParseEnvironment(lookup func(string) string) (Record, error) {
	if lookup == nil {
		return Record{}, errors.New("environment lookup is required")
	}
	record := Record{
		Nickname: lookup(EnvNickname), Biz: lookup(EnvBiz), UIN: lookup(EnvUIN), Key: lookup(EnvKey),
		PassTicket: lookup(EnvPassTicket), WapSID2: lookup(EnvWapSID2), AppMsgToken: lookup(EnvAppMsgToken), Cookie: lookup(EnvCookie),
	}
	if raw := strings.TrimSpace(lookup(EnvExpiresAt)); raw != "" {
		expiresAt, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return Record{}, fmt.Errorf("parse %s: %w", EnvExpiresAt, err)
		}
		record.ExpiresAt = expiresAt
	}
	return ValidateRecord(record)
}

func ValidateRecord(record Record) (Record, error) {
	record.Nickname = strings.TrimSpace(record.Nickname)
	record.Biz = strings.TrimSpace(record.Biz)
	record.UIN = strings.TrimSpace(record.UIN)
	record.Key = strings.TrimSpace(record.Key)
	record.PassTicket = strings.TrimSpace(record.PassTicket)
	record.WapSID2 = strings.TrimSpace(record.WapSID2)
	record.AppMsgToken = strings.TrimSpace(record.AppMsgToken)
	record.Cookie = strings.TrimSpace(record.Cookie)
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "biz", value: record.Biz},
		{name: "uin", value: record.UIN},
		{name: "key", value: record.Key},
		{name: "pass_ticket", value: record.PassTicket},
		{name: "wap_sid2", value: record.WapSID2},
		{name: "appmsg_token", value: record.AppMsgToken},
	} {
		if field.value == "" {
			return Record{}, fmt.Errorf("credential %s is required", field.name)
		}
	}
	for name, value := range map[string]string{
		"nickname": record.Nickname, "biz": record.Biz, "uin": record.UIN, "key": record.Key,
		"pass_ticket": record.PassTicket, "wap_sid2": record.WapSID2, "appmsg_token": record.AppMsgToken, "cookie": record.Cookie,
	} {
		if len(value) > MaximumSecretFieldBytes {
			return Record{}, fmt.Errorf("credential %s exceeds supported limit of %d bytes", name, MaximumSecretFieldBytes)
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return Record{}, fmt.Errorf("credential %s contains invalid control characters", name)
		}
	}
	encoded, err := record.marshalSecret()
	if err != nil {
		return Record{}, err
	}
	if len(encoded) > MaximumCredentialBytes {
		return Record{}, fmt.Errorf("credential record exceeds supported limit of %d bytes", MaximumCredentialBytes)
	}
	return record, nil
}

type Metadata struct {
	ID          string           `json:"id"`
	AccountID   domain.AccountID `json:"accountId"`
	Kind        string           `json:"kind"`
	SecretRef   string           `json:"-"`
	Status      Status           `json:"status"`
	ValidatedAt time.Time        `json:"validatedAt,omitempty"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
}

func (metadata Metadata) MarshalJSON() ([]byte, error) {
	type publicMetadata Metadata
	value := publicMetadata(metadata)
	value.SecretRef = ""
	return json.Marshal(value)
}

type Repository interface {
	UpsertCredential(context.Context, Metadata) (Metadata, error)
	CredentialByID(context.Context, string) (Metadata, error)
	CredentialForAccount(context.Context, domain.AccountID, string) (Metadata, error)
	ListCredentials(context.Context) ([]Metadata, error)
	UpdateCredentialStatus(context.Context, string, Status, time.Time) (Metadata, error)
	RemoveCredential(context.Context, string) error
}

type AccountRepository interface {
	GetAccountByFakeID(context.Context, string) (domain.Account, error)
}

type Validator interface {
	ValidateCredential(context.Context, Record) error
}

type ValidatorFunc func(context.Context, Record) error

func (function ValidatorFunc) ValidateCredential(ctx context.Context, record Record) error {
	return function(ctx, record)
}

type ServiceOptions struct {
	Profile    string
	Repository Repository
	Accounts   AccountRepository
	Secrets    secrets.Store
	Validator  Validator
	Now        func() time.Time
}

type Service struct {
	profile    string
	repository Repository
	accounts   AccountRepository
	secrets    secrets.Store
	validator  Validator
	now        func() time.Time
}

func NewService(options ServiceOptions) *Service {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		profile: strings.TrimSpace(options.Profile), repository: options.Repository, accounts: options.Accounts,
		secrets: options.Secrets, validator: options.Validator, now: now,
	}
}

func (service *Service) Import(ctx context.Context, record Record) (metadata Metadata, err error) {
	record, err = ValidateRecord(record)
	if err != nil {
		return Metadata{}, err
	}
	if service.repository == nil || service.accounts == nil || service.secrets == nil {
		return Metadata{}, errors.New("credential service dependencies are incomplete")
	}
	account, err := service.accounts.GetAccountByFakeID(ctx, record.Biz)
	if err != nil {
		return Metadata{}, fmt.Errorf("associate credential with account %q: %w", record.Biz, err)
	}
	if account.ID == "" || !strings.EqualFold(strings.TrimSpace(account.FakeID), record.Biz) {
		return Metadata{}, fmt.Errorf("associate credential with account %q: account identity mismatch", record.Biz)
	}
	id, err := randomID()
	if err != nil {
		return Metadata{}, err
	}
	secretName := id
	encoded, err := record.marshalSecret()
	if err != nil {
		return Metadata{}, err
	}
	if err := service.secrets.Set(ctx, secretRef(service.profile, secretName), encoded); err != nil {
		return Metadata{}, fmt.Errorf("store credential secret: %w", err)
	}
	defer func() {
		if err != nil {
			_ = service.secrets.Delete(ctx, secretRef(service.profile, secretName))
		}
	}()
	now := service.now()
	metadata, err = service.repository.UpsertCredential(ctx, Metadata{
		ID: id, AccountID: account.ID, Kind: ArticleKind, SecretRef: secretName,
		Status: StatusUnknown, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Metadata{}, fmt.Errorf("store credential metadata: %w", err)
	}
	return metadata, nil
}

func (service *Service) Status(ctx context.Context) ([]Metadata, error) {
	if service.repository == nil {
		return nil, errors.New("credential repository is not configured")
	}
	return service.repository.ListCredentials(ctx)
}

func (service *Service) LoadForAccount(ctx context.Context, accountID domain.AccountID) (Metadata, Record, error) {
	if service.repository == nil || service.secrets == nil {
		return Metadata{}, Record{}, errors.New("credential service dependencies are incomplete")
	}
	metadata, err := service.repository.CredentialForAccount(ctx, accountID, ArticleKind)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrCredentialMissing) {
			return Metadata{}, Record{}, ErrCredentialMissing
		}
		return Metadata{}, Record{}, err
	}
	if metadata.Status == StatusInvalid {
		return metadata, Record{}, ErrCredentialExpired
	}
	encoded, err := service.secrets.Get(ctx, secretRef(service.profile, metadata.SecretRef))
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return metadata, Record{}, ErrCredentialMissing
		}
		return metadata, Record{}, fmt.Errorf("load credential secret: %w", err)
	}
	record, err := unmarshalSecret(encoded)
	if err != nil {
		return metadata, Record{}, err
	}
	if !record.ExpiresAt.IsZero() && !service.now().Before(record.ExpiresAt) {
		return metadata, Record{}, ErrCredentialExpired
	}
	return metadata, record, nil
}

func (service *Service) Validate(ctx context.Context, id string) (Metadata, error) {
	if service.repository == nil || service.secrets == nil || service.validator == nil {
		return Metadata{}, errors.New("credential validator is not configured")
	}
	metadata, err := service.repository.CredentialByID(ctx, strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Metadata{}, ErrCredentialMissing
		}
		return Metadata{}, err
	}
	encoded, err := service.secrets.Get(ctx, secretRef(service.profile, metadata.SecretRef))
	if err != nil {
		return metadata, fmt.Errorf("load credential secret: %w", err)
	}
	record, err := unmarshalSecret(encoded)
	if err == nil && !record.ExpiresAt.IsZero() && !service.now().Before(record.ExpiresAt) {
		err = ErrCredentialExpired
	}
	if err == nil {
		err = service.validator.ValidateCredential(ctx, record)
	}
	status := StatusValid
	if err != nil {
		status = StatusInvalid
	}
	updated, updateErr := service.repository.UpdateCredentialStatus(ctx, metadata.ID, status, service.now())
	if updateErr != nil {
		return Metadata{}, updateErr
	}
	return updated, err
}

// ValidateRecord checks a write-only credential before it is imported. It uses
// the same expiration and upstream validation semantics as Validate, but never
// stores the record, creates metadata, or changes an existing credential.
func (service *Service) ValidateRecord(ctx context.Context, record Record) error {
	if service.validator == nil {
		return errors.New("credential validator is not configured")
	}
	validated, err := ValidateRecord(record)
	if err != nil {
		return err
	}
	if !validated.ExpiresAt.IsZero() && !service.now().Before(validated.ExpiresAt) {
		return ErrCredentialExpired
	}
	return service.validator.ValidateCredential(ctx, validated)
}

func (service *Service) Remove(ctx context.Context, id string) error {
	if service.repository == nil || service.secrets == nil {
		return errors.New("credential service dependencies are incomplete")
	}
	metadata, err := service.repository.CredentialByID(ctx, strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCredentialMissing
		}
		return err
	}
	ref := secretRef(service.profile, metadata.SecretRef)
	encoded, loadErr := service.secrets.Get(ctx, ref)
	if loadErr != nil && !errors.Is(loadErr, secrets.ErrNotFound) {
		return fmt.Errorf("load credential secret before removal: %w", loadErr)
	}
	if err := service.secrets.Delete(ctx, ref); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return fmt.Errorf("remove credential secret: %w", err)
	}
	if err := service.repository.RemoveCredential(ctx, metadata.ID); err != nil {
		if loadErr == nil {
			if restoreErr := service.secrets.Set(ctx, ref, encoded); restoreErr != nil {
				return errors.Join(fmt.Errorf("remove credential metadata: %w", err),
					fmt.Errorf("restore credential secret after metadata failure: %w", restoreErr))
			}
		}
		return fmt.Errorf("remove credential metadata: %w", err)
	}
	return nil
}

func secretRef(profile, name string) secrets.Ref {
	return secrets.Ref{Profile: profile, Kind: ArticleKind, Name: name}
}

func randomID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate credential ID: %w", err)
	}
	return "credential-" + hex.EncodeToString(buffer), nil
}
