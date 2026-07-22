package library

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
)

func TestRouteRepositoryPersistsPolicyAndSeparatesAuthorizationReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routes.sqlite")
	databaseA, err := Open(context.Background(), OpenOptions{Path: path, ProfileID: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = databaseA.Close() })
	databaseB, err := Open(context.Background(), OpenOptions{Path: path, ProfileID: "profile-b"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = databaseB.Close() })

	now := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Millisecond)
	added, err := databaseA.AddRoute(context.Background(), network.RouteConfig{
		ID: "route-a", Name: "trusted", Kind: network.RouteURLWrapper,
		Endpoint: "https://proxy.example/wrap", AuthorizationRef: "secret-ref-a",
		Trust: network.TrustCredential, Classes: []network.RequestClass{network.Comments, network.PublicContent},
		Priority: 10, Enabled: true, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !added.AuthorizationConfigured || added.AuthorizationRef != "secret-ref-a" {
		t.Fatalf("added route authorization metadata = %#v", added)
	}
	encoded, err := json.Marshal(added)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || strings.Contains(string(encoded), "secret-ref-a") {
		t.Fatalf("route JSON leaked authorization reference: %s", encoded)
	}
	if routes, err := databaseB.ListRoutes(context.Background()); err != nil || len(routes) != 0 {
		t.Fatalf("profile-b routes = %#v, %v", routes, err)
	}

	disabled, err := databaseA.SetRouteEnabled(context.Background(), "trusted", false, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled || disabled.Health.State != network.HealthDisabled {
		t.Fatalf("disabled route = %#v", disabled)
	}
	enabled, err := databaseA.SetRouteEnabled(context.Background(), "route-a", true, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.Enabled || enabled.Health.State != network.HealthUnknown {
		t.Fatalf("enabled route = %#v", enabled)
	}
	removed, err := databaseA.RemoveRoute(context.Background(), "trusted")
	if err != nil || removed.ID != "route-a" {
		t.Fatalf("RemoveRoute() = %#v, %v", removed, err)
	}
	if _, err := databaseA.GetRoute(context.Background(), "route-a"); !errors.Is(err, network.ErrRouteNotFound) {
		t.Fatalf("GetRoute(removed) error = %v", err)
	}
}

func TestRouteRepositoryTracksCooldownAndRecoveryProbeState(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, err := database.AddRoute(context.Background(), network.RouteConfig{
		ID: "route-a", Name: "fallback", Kind: network.RouteURLWrapper,
		Endpoint: "https://proxy.example/wrap", Trust: network.TrustPublicOnly,
		Classes: []network.RequestClass{network.PublicContent}, Priority: 50, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := database.RecordRouteHealth(context.Background(), network.HealthSample{
		ID: "sample-1", RouteID: "route-a", Success: false, ErrorClass: "network", SampledAt: now,
	}, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.Health.State != network.HealthDegraded || first.Health.ConsecutiveFailures != 1 {
		t.Fatalf("first failure health = %#v", first.Health)
	}
	second, err := database.RecordRouteHealth(context.Background(), network.HealthSample{
		ID: "sample-2", RouteID: "route-a", Success: false, ErrorClass: "timeout", SampledAt: now.Add(time.Second),
	}, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.Health.State != network.HealthCooldown || second.Health.ConsecutiveFailures != 2 || second.Health.CooldownUntil.IsZero() {
		t.Fatalf("cooldown health = %#v", second.Health)
	}

	if _, err := database.db.Exec("UPDATE network_routes SET cooldown_until=? WHERE id=?", now.Add(-time.Second).UnixMilli(), "route-a"); err != nil {
		t.Fatal(err)
	}
	recovering, err := database.GetRoute(context.Background(), "route-a")
	if err != nil {
		t.Fatal(err)
	}
	if recovering.Health.State != network.HealthRecovering {
		t.Fatalf("expired cooldown health = %#v", recovering.Health)
	}
	recovered, err := database.RecordRouteHealth(context.Background(), network.HealthSample{
		ID: "sample-3", RouteID: "route-a", Success: true, Latency: 25 * time.Millisecond,
		StatusCode: 204, SampledAt: now.Add(2 * time.Second),
	}, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Health.State != network.HealthHealthy || recovered.Health.ConsecutiveFailures != 0 || !recovered.Health.CooldownUntil.IsZero() {
		t.Fatalf("recovered health = %#v", recovered.Health)
	}
}
