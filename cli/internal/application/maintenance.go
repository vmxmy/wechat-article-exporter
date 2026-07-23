package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
)

var ErrMaintenanceConfirmationRequired = errors.New("maintenance confirmation is required")

// MaintenanceService is the application-level facade for settings and local
// maintenance operations. Its collaborators exchange only the DTOs in this
// file: neither presentation adapters nor these seams receive a profile
// runtime, a database handle, a secret store, or host filesystem paths.
type MaintenanceService struct {
	credentials CredentialMaintenance
	proxies     ProxyMaintenance
	preferences PreferencesMaintenance
}

type MaintenanceOptions struct {
	Credentials CredentialMaintenance
	Proxies     ProxyMaintenance
	Preferences PreferencesMaintenance
}

func NewMaintenance(options MaintenanceOptions) *MaintenanceService {
	return &MaintenanceService{
		credentials: options.Credentials,
		proxies:     options.Proxies,
		preferences: options.Preferences,
	}
}

// CredentialMetadata is safe to return from the maintenance facade. It never
// includes credential fields, a secret reference, or a backing-store handle.
type CredentialMetadata struct {
	ID          string    `json:"id"`
	AccountID   string    `json:"accountId"`
	Kind        string    `json:"kind"`
	Status      string    `json:"status"`
	ValidatedAt time.Time `json:"validatedAt,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// CredentialImportRequest accepts a credential only for one-way import. All
// credential material is deliberately excluded from JSON so it cannot be
// accidentally logged or echoed in an adapter response.
type CredentialImportRequest struct {
	Nickname    string    `json:"-"`
	Biz         string    `json:"-"`
	UIN         string    `json:"-"`
	Key         string    `json:"-"`
	PassTicket  string    `json:"-"`
	WapSID2     string    `json:"-"`
	AppMsgToken string    `json:"-"`
	Cookie      string    `json:"-"`
	ExpiresAt   time.Time `json:"-"`
}

// CredentialValidation is deliberately limited to the outcome of a
// write-only validation attempt. It cannot expose credential material, a
// source filename, local paths, session state, or an upstream error body.
type CredentialValidation struct {
	Valid  bool   `json:"valid"`
	Status string `json:"status"`
}

// CredentialMaintenance is intentionally smaller than the credential domain
// service. In particular, it cannot load or validate secret records.
type CredentialMaintenance interface {
	ListCredentialMetadata(context.Context) ([]CredentialMetadata, error)
	ValidateCredential(context.Context, CredentialImportRequest) (CredentialValidation, error)
	ImportCredential(context.Context, CredentialImportRequest) (CredentialMetadata, error)
	RemoveCredential(context.Context, string) error
}

func (service *MaintenanceService) ValidateCredential(ctx context.Context, request CredentialImportRequest) (CredentialValidation, error) {
	if service.credentials == nil {
		return CredentialValidation{}, fmt.Errorf("validate credential: %w", ErrUnavailable)
	}
	return service.credentials.ValidateCredential(ctx, request)
}

func (service *MaintenanceService) Credentials(ctx context.Context) ([]CredentialMetadata, error) {
	if service.credentials == nil {
		return nil, fmt.Errorf("list credentials: %w", ErrUnavailable)
	}
	return service.credentials.ListCredentialMetadata(ctx)
}

func (service *MaintenanceService) ImportCredential(ctx context.Context, request CredentialImportRequest) (CredentialMetadata, error) {
	if service.credentials == nil {
		return CredentialMetadata{}, fmt.Errorf("import credential: %w", ErrUnavailable)
	}
	return service.credentials.ImportCredential(ctx, request)
}

func (service *MaintenanceService) RemoveCredential(ctx context.Context, id string) error {
	if service.credentials == nil {
		return fmt.Errorf("remove credential: %w", ErrUnavailable)
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("credential identifier is required")
	}
	return service.credentials.RemoveCredential(ctx, strings.TrimSpace(id))
}

type ProxyTrust string

const (
	ProxyTrustPublicOnly        ProxyTrust = ProxyTrust(network.TrustPublicOnly)
	ProxyTrustCredentialTrusted ProxyTrust = ProxyTrust(network.TrustCredential)
)

type ProxyRequestClass string

const (
	ProxyRequestClassPublicContent     ProxyRequestClass = ProxyRequestClass(network.PublicContent)
	ProxyRequestClassPublicResource    ProxyRequestClass = ProxyRequestClass(network.PublicResource)
	ProxyRequestClassManagementSession ProxyRequestClass = ProxyRequestClass(network.ManagementSession)
	ProxyRequestClassArticleCredential ProxyRequestClass = ProxyRequestClass(network.ArticleCredential)
	ProxyRequestClassEngagementMetrics ProxyRequestClass = ProxyRequestClass(network.EngagementMetrics)
	ProxyRequestClassComments          ProxyRequestClass = ProxyRequestClass(network.Comments)
	ProxyRequestClassPaidContent       ProxyRequestClass = ProxyRequestClass(network.PaidContent)
)

type ProxyRoute struct {
	ID                      string              `json:"id"`
	Name                    string              `json:"name"`
	Endpoint                string              `json:"endpoint"`
	AuthorizationConfigured bool                `json:"authorizationConfigured"`
	Trust                   ProxyTrust          `json:"trust"`
	Classes                 []ProxyRequestClass `json:"classes"`
	Priority                int                 `json:"priority"`
	Enabled                 bool                `json:"enabled"`
	Health                  ProxyHealth         `json:"health"`
	CreatedAt               time.Time           `json:"createdAt"`
	UpdatedAt               time.Time           `json:"updatedAt"`
}

type ProxyHealth struct {
	State               string        `json:"state"`
	ConsecutiveFailures int           `json:"consecutiveFailures"`
	CooldownUntil       time.Time     `json:"cooldownUntil,omitempty"`
	LastSampleAt        time.Time     `json:"lastSampleAt,omitempty"`
	LastSuccessAt       time.Time     `json:"lastSuccessAt,omitempty"`
	LastLatency         time.Duration `json:"lastLatency,omitempty"`
	LastStatusCode      int           `json:"lastStatusCode,omitempty"`
	LastErrorClass      string        `json:"lastErrorClass,omitempty"`
}

type ProxyAddRequest struct {
	Name          string              `json:"name"`
	Endpoint      string              `json:"endpoint"`
	Authorization string              `json:"-"`
	Trust         ProxyTrust          `json:"trust"`
	Classes       []ProxyRequestClass `json:"classes"`
	Priority      int                 `json:"priority"`
	Confirmation  string              `json:"-"`
}

// ProxyDisclosure tells a caller exactly which credential material a trusted
// route may receive and the exact proof required to create it.
type ProxyDisclosure struct {
	Required     bool     `json:"required"`
	Confirmation string   `json:"confirmation,omitempty"`
	Secrets      []string `json:"secrets,omitempty"`
}

type ProxyProbeResult struct {
	Route              ProxyRoute    `json:"route"`
	Latency            time.Duration `json:"latency"`
	StatusCode         int           `json:"statusCode,omitempty"`
	ResponseValid      bool          `json:"responseValid"`
	CredentialEligible bool          `json:"credentialEligible"`
	ErrorClass         string        `json:"errorClass,omitempty"`
}

// ProxyMaintenance deliberately does not expose proxy authorizations or
// network transport objects. AddProxy receives authorization only as a
// write-only request field.
type ProxyMaintenance interface {
	ListProxies(context.Context) ([]ProxyRoute, error)
	AddProxy(context.Context, ProxyAddRequest) (ProxyRoute, error)
	RemoveProxy(context.Context, string) (ProxyRoute, error)
	SetProxyEnabled(context.Context, string, bool) (ProxyRoute, error)
	TestProxy(context.Context, string) (ProxyProbeResult, error)
}

func (service *MaintenanceService) Proxies(ctx context.Context) ([]ProxyRoute, error) {
	if service.proxies == nil {
		return nil, fmt.Errorf("list proxies: %w", ErrUnavailable)
	}
	routes, err := service.proxies.ListProxies(ctx)
	for index := range routes {
		routes[index] = sanitizeProxyRoute(routes[index])
	}
	return routes, err
}

func (service *MaintenanceService) ProxyDisclosure(request ProxyAddRequest) (ProxyDisclosure, error) {
	normalized, err := normalizeProxyAddRequest(request)
	if err != nil {
		return ProxyDisclosure{}, err
	}
	return proxyDisclosure(normalized), nil
}

func (service *MaintenanceService) AddProxy(ctx context.Context, request ProxyAddRequest) (ProxyRoute, error) {
	if service.proxies == nil {
		return ProxyRoute{}, fmt.Errorf("add proxy: %w", ErrUnavailable)
	}
	normalized, err := normalizeProxyAddRequest(request)
	if err != nil {
		return ProxyRoute{}, err
	}
	disclosure := proxyDisclosure(normalized)
	if disclosure.Required && normalized.Confirmation != disclosure.Confirmation {
		return ProxyRoute{}, fmt.Errorf("%w: use %q", ErrMaintenanceConfirmationRequired, disclosure.Confirmation)
	}
	route, err := service.proxies.AddProxy(ctx, normalized)
	return sanitizeProxyRoute(route), err
}

func (service *MaintenanceService) RemoveProxy(ctx context.Context, idOrName string) (ProxyRoute, error) {
	if service.proxies == nil {
		return ProxyRoute{}, fmt.Errorf("remove proxy: %w", ErrUnavailable)
	}
	return service.proxyOperation(ctx, "remove proxy", idOrName, service.proxies.RemoveProxy)
}

func (service *MaintenanceService) EnableProxy(ctx context.Context, idOrName string) (ProxyRoute, error) {
	if service.proxies == nil {
		return ProxyRoute{}, fmt.Errorf("enable proxy: %w", ErrUnavailable)
	}
	return service.proxyOperation(ctx, "enable proxy", idOrName, func(ctx context.Context, id string) (ProxyRoute, error) {
		return service.proxies.SetProxyEnabled(ctx, id, true)
	})
}

func (service *MaintenanceService) DisableProxy(ctx context.Context, idOrName string) (ProxyRoute, error) {
	if service.proxies == nil {
		return ProxyRoute{}, fmt.Errorf("disable proxy: %w", ErrUnavailable)
	}
	return service.proxyOperation(ctx, "disable proxy", idOrName, func(ctx context.Context, id string) (ProxyRoute, error) {
		return service.proxies.SetProxyEnabled(ctx, id, false)
	})
}

func (service *MaintenanceService) TestProxy(ctx context.Context, idOrName string) (ProxyProbeResult, error) {
	if service.proxies == nil {
		return ProxyProbeResult{}, fmt.Errorf("test proxy: %w", ErrUnavailable)
	}
	idOrName = strings.TrimSpace(idOrName)
	if idOrName == "" {
		return ProxyProbeResult{}, errors.New("proxy identifier is required")
	}
	result, err := service.proxies.TestProxy(ctx, idOrName)
	result.Route = sanitizeProxyRoute(result.Route)
	return result, err
}

func (service *MaintenanceService) proxyOperation(ctx context.Context, operation, idOrName string, call func(context.Context, string) (ProxyRoute, error)) (ProxyRoute, error) {
	idOrName = strings.TrimSpace(idOrName)
	if idOrName == "" {
		return ProxyRoute{}, errors.New("proxy identifier is required")
	}
	route, err := call(ctx, idOrName)
	return sanitizeProxyRoute(route), err
}

func normalizeProxyAddRequest(request ProxyAddRequest) (ProxyAddRequest, error) {
	request.Name = strings.TrimSpace(request.Name)
	request.Endpoint = strings.TrimSpace(request.Endpoint)
	if request.Name == "" {
		return ProxyAddRequest{}, errors.New("proxy name is required")
	}
	if request.Endpoint == "" {
		return ProxyAddRequest{}, errors.New("proxy endpoint is required")
	}
	if request.Trust == "" {
		request.Trust = ProxyTrustPublicOnly
	}
	if _, err := network.ParseTrustLevel(string(request.Trust)); err != nil {
		return ProxyAddRequest{}, err
	}
	classes := make([]network.RequestClass, len(request.Classes))
	for index, class := range request.Classes {
		classes[index] = network.RequestClass(class)
	}
	normalized, err := network.NormalizeRequestClasses(classes)
	if err != nil {
		return ProxyAddRequest{}, err
	}
	if err := network.ValidateClassTrust(normalized, network.TrustLevel(request.Trust)); err != nil {
		return ProxyAddRequest{}, err
	}
	request.Classes = make([]ProxyRequestClass, len(normalized))
	for index, class := range normalized {
		request.Classes[index] = ProxyRequestClass(class)
	}
	if request.Priority == 0 {
		request.Priority = 100
	}
	if request.Priority < 1 || request.Priority > 10000 {
		return ProxyAddRequest{}, errors.New("proxy priority must be between 1 and 10000")
	}
	return request, nil
}

func proxyDisclosure(request ProxyAddRequest) ProxyDisclosure {
	if request.Trust != ProxyTrustCredentialTrusted {
		return ProxyDisclosure{}
	}
	classes := make([]network.RequestClass, len(request.Classes))
	for index, class := range request.Classes {
		classes[index] = network.RequestClass(class)
	}
	return ProxyDisclosure{
		Required:     true,
		Confirmation: network.CredentialTrustConfirmation(request.Name),
		Secrets:      network.CredentialSecretsForClasses(classes),
	}
}

func sanitizeProxyRoute(route ProxyRoute) ProxyRoute {
	route.Endpoint = safety.RedactURL(route.Endpoint)
	return route
}

// Preferences intentionally omits export roots and every other host path.
// Path capabilities belong to a separate application boundary.
type Preferences struct {
	Sync     SyncPreferences     `json:"sync"`
	Download DownloadPreferences `json:"download"`
	Export   ExportPreferences   `json:"export"`
	Display  DisplayPreferences  `json:"display"`
	Proxy    ProxyPreferences    `json:"proxy"`
}

type SyncPreferences struct {
	Range             string        `json:"range"`
	DatePoint         time.Time     `json:"datePoint,omitempty"`
	PageDelay         time.Duration `json:"pageDelay"`
	Jitter            time.Duration `json:"jitter"`
	PageSize          int           `json:"pageSize"`
	Incremental       bool          `json:"incremental"`
	UnsafePacingSaved bool          `json:"unsafePacingSaved"`
}

type DownloadPreferences struct {
	Concurrency              int  `json:"concurrency"`
	ForceContent             bool `json:"forceContent"`
	MetadataOverridesContent bool `json:"metadataOverridesContent"`
}

type ExportPreferences struct {
	NamingTemplate      string `json:"namingTemplate"`
	MaximumNameBytes    int    `json:"maximumNameBytes"`
	CollisionPolicy     string `json:"collisionPolicy"`
	ExcelIncludeContent bool   `json:"excelIncludeContent"`
	JSONIncludeContent  bool   `json:"jsonIncludeContent"`
	JSONIncludeComments bool   `json:"jsonIncludeComments"`
	HTMLIncludeComments bool   `json:"htmlIncludeComments"`
}

type DisplayPreferences struct {
	NoColor     bool   `json:"noColor"`
	ASCII       bool   `json:"ascii"`
	Plain       bool   `json:"plain"`
	HideDeleted bool   `json:"hideDeleted"`
	Language    string `json:"language,omitempty"`
}

type ProxyPreferences struct {
	DirectFirst     bool `json:"directFirst"`
	FallbackEnabled bool `json:"fallbackEnabled"`
}

// PreferencesPatch distinguishes an absent setting from a requested zero or
// false value. The implementation owns validation and atomic persistence.
type PreferencesPatch struct {
	Sync     *SyncPreferencesPatch     `json:"sync,omitempty"`
	Download *DownloadPreferencesPatch `json:"download,omitempty"`
	Export   *ExportPreferencesPatch   `json:"export,omitempty"`
	Display  *DisplayPreferencesPatch  `json:"display,omitempty"`
	Proxy    *ProxyPreferencesPatch    `json:"proxy,omitempty"`
}

type SyncPreferencesPatch struct {
	Range             *string        `json:"range,omitempty"`
	DatePoint         *time.Time     `json:"datePoint,omitempty"`
	PageDelay         *time.Duration `json:"pageDelay,omitempty"`
	Jitter            *time.Duration `json:"jitter,omitempty"`
	PageSize          *int           `json:"pageSize,omitempty"`
	Incremental       *bool          `json:"incremental,omitempty"`
	UnsafePacingSaved *bool          `json:"unsafePacingSaved,omitempty"`
}

type DownloadPreferencesPatch struct {
	Concurrency              *int  `json:"concurrency,omitempty"`
	ForceContent             *bool `json:"forceContent,omitempty"`
	MetadataOverridesContent *bool `json:"metadataOverridesContent,omitempty"`
}

type ExportPreferencesPatch struct {
	NamingTemplate      *string `json:"namingTemplate,omitempty"`
	MaximumNameBytes    *int    `json:"maximumNameBytes,omitempty"`
	CollisionPolicy     *string `json:"collisionPolicy,omitempty"`
	ExcelIncludeContent *bool   `json:"excelIncludeContent,omitempty"`
	JSONIncludeContent  *bool   `json:"jsonIncludeContent,omitempty"`
	JSONIncludeComments *bool   `json:"jsonIncludeComments,omitempty"`
	HTMLIncludeComments *bool   `json:"htmlIncludeComments,omitempty"`
}

type DisplayPreferencesPatch struct {
	NoColor     *bool   `json:"noColor,omitempty"`
	ASCII       *bool   `json:"ascii,omitempty"`
	Plain       *bool   `json:"plain,omitempty"`
	HideDeleted *bool   `json:"hideDeleted,omitempty"`
	Language    *string `json:"language,omitempty"`
}

type ProxyPreferencesPatch struct {
	DirectFirst     *bool `json:"directFirst,omitempty"`
	FallbackEnabled *bool `json:"fallbackEnabled,omitempty"`
}

type PreferencesMaintenance interface {
	Preferences(context.Context) (Preferences, error)
	PatchPreferences(context.Context, PreferencesPatch) (Preferences, error)
}

func (service *MaintenanceService) Preferences(ctx context.Context) (Preferences, error) {
	if service.preferences == nil {
		return Preferences{}, fmt.Errorf("read preferences: %w", ErrUnavailable)
	}
	return service.preferences.Preferences(ctx)
}

func (service *MaintenanceService) PatchPreferences(ctx context.Context, patch PreferencesPatch) (Preferences, error) {
	if service.preferences == nil {
		return Preferences{}, fmt.Errorf("patch preferences: %w", ErrUnavailable)
	}
	if patch.empty() {
		return Preferences{}, errors.New("preferences patch is empty")
	}
	return service.preferences.PatchPreferences(ctx, patch)
}

func (patch PreferencesPatch) empty() bool {
	return patch.Sync == nil && patch.Download == nil && patch.Export == nil && patch.Display == nil && patch.Proxy == nil
}

// MaintenancePlan and MaintenanceConfirmation reserve the durable,
// operation-specific maintenance contract. They deliberately do not contain
// paths or backend handles; future operations can use these DTOs unchanged.
type MaintenancePlan struct {
	Operation    string    `json:"operation"`
	Summary      string    `json:"summary"`
	Destructive  bool      `json:"destructive"`
	Confirmation string    `json:"confirmation,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
}

type MaintenanceConfirmation struct {
	Operation string `json:"operation"`
	Value     string `json:"value"`
}
