package network_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

type fixedResolver map[string][]net.IPAddr

func (resolver fixedResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	addresses, ok := resolver[host]
	if !ok {
		return nil, fmt.Errorf("unexpected host %s", host)
	}
	return addresses, nil
}

type dynamicResolver struct{}

func (dynamicResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	if host == "mp.weixin.qq.com" {
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
	}
	addresses, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	result := make([]net.IPAddr, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, net.IPAddr{IP: address})
	}
	return result, nil
}

type inspectableSecrets struct {
	values  map[secrets.Ref][]byte
	sets    []secrets.Ref
	deletes []secrets.Ref
}

func newInspectableSecrets() *inspectableSecrets {
	return &inspectableSecrets{values: make(map[secrets.Ref][]byte)}
}

func (*inspectableSecrets) Backend() string { return "inspectable" }
func (store *inspectableSecrets) Get(_ context.Context, ref secrets.Ref) ([]byte, error) {
	value, ok := store.values[ref]
	if !ok {
		return nil, secrets.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}
func (store *inspectableSecrets) Set(_ context.Context, ref secrets.Ref, value []byte) error {
	store.sets = append(store.sets, ref)
	store.values[ref] = append([]byte(nil), value...)
	return nil
}
func (store *inspectableSecrets) Delete(_ context.Context, ref secrets.Ref) error {
	store.deletes = append(store.deletes, ref)
	delete(store.values, ref)
	return nil
}
func (store *inspectableSecrets) DeleteProfile(profile string) error {
	for ref := range store.values {
		if ref.Profile == profile {
			delete(store.values, ref)
		}
	}
	return nil
}

type advancingClock struct{ current time.Time }

func (clock *advancingClock) Now() time.Time {
	value := clock.current
	clock.current = clock.current.Add(10 * time.Millisecond)
	return value
}

type staticClient struct {
	name string
	call func(context.Context, network.Request) (network.Result, error)
}

func (client staticClient) Name() string { return client.name }
func (client staticClient) Do(ctx context.Context, request network.Request) (network.Result, error) {
	return client.call(ctx, request)
}

func TestServiceSecretLifecycleAndRouteJSON(t *testing.T) {
	database := openRouteDatabase(t)
	secretStore := newInspectableSecrets()
	service := network.NewService(network.ServiceOptions{
		Profile: "profile-a", Routes: database, Secrets: secretStore,
		EndpointPolicy: publicEndpointPolicy("proxy.example"),
	})
	route, err := service.Add(context.Background(), network.AddProxyRequest{
		Name: "authorized", Endpoint: "https://proxy.example/wrap?authorization=url-secret",
		Authorization: "proxy-secret", Trust: network.TrustPublicOnly,
		Classes: []network.RequestClass{network.PublicResource, network.PublicContent}, Priority: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(secretStore.sets) != 1 || len(secretStore.values) != 1 {
		t.Fatalf("secret writes = %#v values=%d", secretStore.sets, len(secretStore.values))
	}
	stored := secretStore.values[secretStore.sets[0]]
	if string(stored) != "proxy-secret" || route.AuthorizationRef != secretStore.sets[0].Name {
		t.Fatalf("stored secret=%q route=%#v", stored, route)
	}
	jsonBytes, jsonErr := route.MarshalJSON()
	if jsonErr != nil {
		t.Fatal(jsonErr)
	}
	for _, forbidden := range []string{"proxy-secret", "url-secret", `"authorizationRef"`} {
		if strings.Contains(string(jsonBytes), forbidden) {
			t.Fatalf("route JSON leaked %q: json=%s", forbidden, jsonBytes)
		}
	}
	if _, err := service.Remove(context.Background(), route.ID); err != nil {
		t.Fatal(err)
	}
	if len(secretStore.deletes) != 1 || len(secretStore.values) != 0 {
		t.Fatalf("secret deletes = %#v values=%d", secretStore.deletes, len(secretStore.values))
	}
}

func TestServiceProbeUsesSyntheticTargetAndNoWeChatCredentials(t *testing.T) {
	database := openRouteDatabase(t)
	secretStore := newInspectableSecrets()
	var seenURL string
	var seenHeaders string
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seenURL = request.URL.Query().Get("url")
		seenHeaders = request.URL.Query().Get("headers")
		if request.URL.Query().Get("authorization") != "proxy-secret" {
			t.Fatalf("proxy authorization missing")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	endpoint, _ := url.Parse(proxy.URL)
	resolver := dynamicResolver{}
	clock := &advancingClock{current: time.Now().UTC()}
	probeTarget, _ := url.Parse("https://mp.weixin.qq.com/robots.txt")
	service := network.NewService(network.ServiceOptions{
		Profile: "profile-a", Routes: database, Secrets: secretStore, HTTP: proxy.Client(),
		EndpointPolicy: network.DestinationPolicy{
			AllowedHosts: map[string]struct{}{endpoint.Hostname(): {}}, AllowLoopback: true, Resolver: resolver,
		},
		TargetPolicy: network.DestinationPolicy{
			AllowedHosts: map[string]struct{}{"mp.weixin.qq.com": {}}, Resolver: resolver,
		},
		ProbeTarget: probeTarget, Now: clock.Now,
	})
	route, err := service.Add(context.Background(), network.AddProxyRequest{
		Name: "trusted", Endpoint: proxy.URL, Authorization: "proxy-secret",
		Trust: network.TrustCredential, Classes: []network.RequestClass{network.Comments}, Priority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Test(context.Background(), route.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ResponseValid || result.StatusCode != http.StatusNoContent || !result.CredentialEligible {
		t.Fatalf("probe result = %#v", result)
	}
	if seenURL != probeTarget.String() {
		t.Fatalf("probe target = %q", seenURL)
	}
	for _, forbidden := range []string{"Cookie", "Authorization", "pass_ticket", "appmsg_token", "key="} {
		if strings.Contains(seenHeaders, forbidden) || strings.Contains(seenURL, forbidden) {
			t.Fatalf("probe sent WeChat credential marker %q: url=%q headers=%q", forbidden, seenURL, seenHeaders)
		}
	}
}

func TestServiceProbeErrorsRedactAuthorizationAndTargetQuery(t *testing.T) {
	database := openRouteDatabase(t)
	secretStore := newInspectableSecrets()
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint, _ := url.Parse(proxy.URL)
	resolver := dynamicResolver{}
	probeTarget, _ := url.Parse("https://mp.weixin.qq.com/robots.txt?pass_ticket=wechat-secret&safe=context")
	failingHTTP := doerFunc(func(request *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("dial failed for %s", request.URL.String())
	})
	service := network.NewService(network.ServiceOptions{
		Profile: "profile-a", Routes: database, Secrets: secretStore, HTTP: failingHTTP,
		EndpointPolicy: network.DestinationPolicy{
			AllowedHosts: map[string]struct{}{endpoint.Hostname(): {}}, AllowLoopback: true, Resolver: resolver,
		},
		TargetPolicy: network.DestinationPolicy{AllowedHosts: map[string]struct{}{"mp.weixin.qq.com": {}}, Resolver: resolver},
		ProbeTarget:  probeTarget, FailureThreshold: 1,
	})
	route, err := service.Add(context.Background(), network.AddProxyRequest{
		Name: "redacted", Endpoint: proxy.URL + "/wrap?authorization=url-secret", Authorization: "proxy-secret",
		Trust: network.TrustPublicOnly, Classes: []network.RequestClass{network.PublicContent},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Test(context.Background(), route.ID)
	if err == nil {
		t.Fatal("Test() error = nil")
	}
	message := err.Error()
	for _, forbidden := range []string{"proxy-secret", "url-secret", "wechat-secret"} {
		if strings.Contains(message, forbidden) {
			t.Fatalf("probe error leaked %q: %s", forbidden, message)
		}
	}
	if !strings.Contains(message, "authorization=%5BREDACTED%5D") && !strings.Contains(message, "authorization=[REDACTED]") {
		t.Fatalf("probe error lost authorization redaction: %s", message)
	}
	updated, err := database.GetRoute(context.Background(), route.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(updated.Health.LastErrorClass, "secret") || updated.Health.LastErrorClass != "network" {
		t.Fatalf("stored error class = %q", updated.Health.LastErrorClass)
	}
}

func TestCooldownRecoveryProbeThenHealthySelection(t *testing.T) {
	database := openRouteDatabase(t)
	secretStore := newInspectableSecrets()
	proxyCalls := 0
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		proxyCalls++
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	endpoint, _ := url.Parse(proxy.URL)
	resolver := dynamicResolver{}
	clock := &advancingClock{current: time.Now().UTC()}
	probeTarget, _ := url.Parse("https://mp.weixin.qq.com/robots.txt")
	service := network.NewService(network.ServiceOptions{
		Profile: "profile-a", Routes: database, Secrets: secretStore, HTTP: proxy.Client(),
		EndpointPolicy: network.DestinationPolicy{
			AllowedHosts: map[string]struct{}{endpoint.Hostname(): {}}, AllowLoopback: true, Resolver: resolver,
		},
		TargetPolicy: network.DestinationPolicy{AllowedHosts: map[string]struct{}{"mp.weixin.qq.com": {}}, Resolver: resolver},
		ProbeTarget:  probeTarget, Now: clock.Now, FailureThreshold: 1, Cooldown: time.Minute,
	})
	route, err := service.Add(context.Background(), network.AddProxyRequest{
		Name: "recover", Endpoint: proxy.URL, Trust: network.TrustPublicOnly,
		Classes: []network.RequestClass{network.PublicContent}, Priority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	failureAt := clock.current
	if _, err := database.RecordRouteHealth(context.Background(), network.HealthSample{
		RouteID: route.ID, Success: false, ErrorClass: "network", SampledAt: failureAt,
	}, 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	cooldownCandidates, err := service.Candidates(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (network.Router{Routes: cooldownCandidates, Now: clock.Now}).Do(context.Background(), network.Request{Class: network.PublicContent}); err == nil {
		t.Fatal("router selected route during cooldown")
	}
	if proxyCalls != 0 {
		t.Fatalf("proxy called during cooldown: %d", proxyCalls)
	}

	clock.current = failureAt.Add(2 * time.Minute)
	recoveryCandidates, err := service.Candidates(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse("https://mp.weixin.qq.com/s/example")
	result, err := (network.Router{Routes: recoveryCandidates, Now: clock.Now}).Do(context.Background(), network.Request{
		Class: network.PublicContent, URL: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Route != "recover" || proxyCalls != 2 {
		t.Fatalf("recovery result=%#v proxyCalls=%d", result, proxyCalls)
	}
	recovered, err := database.GetRoute(context.Background(), route.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Health.State != network.HealthHealthy || recovered.Health.ConsecutiveFailures != 0 {
		t.Fatalf("recovered health = %#v", recovered.Health)
	}
}

func TestRouterPriorityAndClassFiltering(t *testing.T) {
	var calls []string
	router := network.Router{Routes: []network.Candidate{
		{Client: staticClient{name: "wrong-class", call: recordSuccess(&calls, "wrong-class")}, Enabled: true,
			Priority: 1, Trust: network.TrustPublicOnly, Classes: network.ClassesMap([]network.RequestClass{network.PublicResource})},
		{Client: staticClient{name: "lower", call: recordSuccess(&calls, "lower")}, Enabled: true,
			Priority: 20, Trust: network.TrustPublicOnly, Classes: network.ClassesMap([]network.RequestClass{network.PublicContent})},
		{Client: staticClient{name: "higher", call: recordSuccess(&calls, "higher")}, Enabled: true,
			Priority: 10, Trust: network.TrustPublicOnly, Classes: network.ClassesMap([]network.RequestClass{network.PublicContent})},
	}}
	result, err := router.Do(context.Background(), network.Request{Class: network.PublicContent})
	if err != nil {
		t.Fatal(err)
	}
	if result.Route != "higher" || !reflect.DeepEqual(calls, []string{"higher"}) {
		t.Fatalf("route=%q calls=%v", result.Route, calls)
	}
}

func openRouteDatabase(t *testing.T) *library.Database {
	t.Helper()
	database, err := library.Open(context.Background(), library.OpenOptions{
		Path: filepath.Join(t.TempDir(), "routes.sqlite3"), ProfileID: "profile-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func publicEndpointPolicy(host string) network.DestinationPolicy {
	return network.DestinationPolicy{
		AllowedHosts: map[string]struct{}{host: {}},
		Resolver:     fixedResolver{host: {{IP: net.ParseIP("203.0.113.20")}}},
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) { return function(request) }

func recordSuccess(calls *[]string, name string) func(context.Context, network.Request) (network.Result, error) {
	return func(context.Context, network.Request) (network.Result, error) {
		*calls = append(*calls, name)
		return network.Result{Route: name, Response: &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header),
		}}, nil
	}
}
