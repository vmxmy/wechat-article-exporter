package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
)

const routeHealthHistoryLimit = 50

func (database *Database) AddRoute(ctx context.Context, route network.RouteConfig) (network.RouteConfig, error) {
	classes, err := network.NormalizeRequestClasses(route.Classes)
	if err != nil {
		return network.RouteConfig{}, err
	}
	if err := network.ValidateClassTrust(classes, route.Trust); err != nil {
		return network.RouteConfig{}, err
	}
	encodedClasses, err := json.Marshal(classes)
	if err != nil {
		return network.RouteConfig{}, fmt.Errorf("encode route request classes: %w", err)
	}
	now := route.UpdatedAt
	if now.IsZero() {
		now = time.Now()
	}
	created := route.CreatedAt
	if created.IsZero() {
		created = now
	}
	_, err = database.db.ExecContext(ctx, `INSERT INTO network_routes(
id, profile_id, name, kind, endpoint, authorization_ref, trust_level, request_classes,
priority, enabled, cooldown_until, consecutive_failures, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, 0, ?, ?)`,
		route.ID, database.profileID, route.Name, route.Kind, route.Endpoint, route.AuthorizationRef,
		route.Trust, string(encodedClasses), route.Priority, route.Enabled, created.UnixMilli(), now.UnixMilli())
	if err != nil {
		return network.RouteConfig{}, fmt.Errorf("add network route: %w", err)
	}
	return database.GetRoute(ctx, route.ID)
}

func (database *Database) ListRoutes(ctx context.Context) ([]network.RouteConfig, error) {
	rows, err := database.db.QueryContext(ctx, `SELECT
r.id, r.name, r.kind, r.endpoint, r.authorization_ref, r.trust_level, r.request_classes,
r.priority, r.enabled, r.cooldown_until, r.consecutive_failures, r.created_at, r.updated_at,
h.success, h.latency_ms, h.status_code, h.error_class, h.sampled_at,
(SELECT MAX(sampled_at) FROM route_health_samples s WHERE s.route_id=r.id AND s.success=1)
FROM network_routes r
LEFT JOIN route_health_samples h ON h.id=(
  SELECT id FROM route_health_samples s WHERE s.route_id=r.id ORDER BY sampled_at DESC, id DESC LIMIT 1
)
WHERE r.profile_id=?
ORDER BY r.priority, r.name COLLATE NOCASE, r.id`, database.profileID)
	if err != nil {
		return nil, fmt.Errorf("list network routes: %w", err)
	}
	defer rows.Close()
	routes := make([]network.RouteConfig, 0)
	for rows.Next() {
		route, scanErr := scanRoute(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

func (database *Database) GetRoute(ctx context.Context, idOrName string) (network.RouteConfig, error) {
	row := database.db.QueryRowContext(ctx, `SELECT
r.id, r.name, r.kind, r.endpoint, r.authorization_ref, r.trust_level, r.request_classes,
r.priority, r.enabled, r.cooldown_until, r.consecutive_failures, r.created_at, r.updated_at,
h.success, h.latency_ms, h.status_code, h.error_class, h.sampled_at,
(SELECT MAX(sampled_at) FROM route_health_samples s WHERE s.route_id=r.id AND s.success=1)
FROM network_routes r
LEFT JOIN route_health_samples h ON h.id=(
  SELECT id FROM route_health_samples s WHERE s.route_id=r.id ORDER BY sampled_at DESC, id DESC LIMIT 1
)
WHERE r.profile_id=? AND (r.id=? OR r.name=?)
ORDER BY CASE WHEN r.id=? THEN 0 ELSE 1 END
LIMIT 1`, database.profileID, idOrName, idOrName, idOrName)
	route, err := scanRoute(row)
	if errors.Is(err, sql.ErrNoRows) {
		return network.RouteConfig{}, fmt.Errorf("route %q: %w", idOrName, network.ErrRouteNotFound)
	}
	return route, err
}

func (database *Database) RemoveRoute(ctx context.Context, idOrName string) (network.RouteConfig, error) {
	route, err := database.GetRoute(ctx, idOrName)
	if err != nil {
		return network.RouteConfig{}, err
	}
	result, err := database.db.ExecContext(ctx, "DELETE FROM network_routes WHERE profile_id=? AND id=?", database.profileID, route.ID)
	if err != nil {
		return network.RouteConfig{}, fmt.Errorf("remove network route: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return network.RouteConfig{}, err
	}
	if affected != 1 {
		return network.RouteConfig{}, fmt.Errorf("route %q: %w", idOrName, network.ErrRouteNotFound)
	}
	return route, nil
}

func (database *Database) SetRouteEnabled(ctx context.Context, idOrName string, enabled bool, updatedAt time.Time) (network.RouteConfig, error) {
	route, err := database.GetRoute(ctx, idOrName)
	if err != nil {
		return network.RouteConfig{}, err
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	_, err = database.db.ExecContext(ctx, "UPDATE network_routes SET enabled=?, updated_at=? WHERE profile_id=? AND id=?",
		enabled, updatedAt.UnixMilli(), database.profileID, route.ID)
	if err != nil {
		return network.RouteConfig{}, fmt.Errorf("update network route: %w", err)
	}
	return database.GetRoute(ctx, route.ID)
}

func (database *Database) RecordRouteHealth(ctx context.Context, sample network.HealthSample, failureThreshold int, cooldown time.Duration) (network.RouteConfig, error) {
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	if cooldown <= 0 {
		cooldown = 5 * time.Minute
	}
	if sample.SampledAt.IsZero() {
		sample.SampledAt = time.Now()
	}
	route, err := database.GetRoute(ctx, sample.RouteID)
	if err != nil {
		return network.RouteConfig{}, err
	}
	if sample.ID == "" {
		sample.ID = route.ID + "-" + fmt.Sprint(sample.SampledAt.UnixNano())
	}
	err = database.WithTx(ctx, func(transaction *sql.Tx) error {
		if _, executeErr := transaction.ExecContext(ctx, `INSERT INTO route_health_samples(
id, route_id, success, latency_ms, status_code, error_class, sampled_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
			sample.ID, route.ID, sample.Success, durationMillis(sample.Latency), sample.StatusCode,
			sample.ErrorClass, sample.SampledAt.UnixMilli()); executeErr != nil {
			return fmt.Errorf("record route health sample: %w", executeErr)
		}
		failures := route.Health.ConsecutiveFailures
		var cooldownUntil any
		if sample.Success {
			failures = 0
		} else {
			failures++
			if failures >= failureThreshold {
				cooldownUntil = sample.SampledAt.Add(cooldown).UnixMilli()
			}
		}
		if _, executeErr := transaction.ExecContext(ctx, `UPDATE network_routes
SET consecutive_failures=?, cooldown_until=?, updated_at=? WHERE profile_id=? AND id=?`,
			failures, cooldownUntil, sample.SampledAt.UnixMilli(), database.profileID, route.ID); executeErr != nil {
			return fmt.Errorf("update route health: %w", executeErr)
		}
		_, executeErr := transaction.ExecContext(ctx, `DELETE FROM route_health_samples WHERE route_id=? AND id NOT IN (
SELECT id FROM route_health_samples WHERE route_id=? ORDER BY sampled_at DESC, id DESC LIMIT ?
)`, route.ID, route.ID, routeHealthHistoryLimit)
		return executeErr
	})
	if err != nil {
		return network.RouteConfig{}, err
	}
	return database.GetRoute(ctx, route.ID)
}

func (database *Database) ResetRouteHealth(ctx context.Context, idOrName string, updatedAt time.Time) (network.RouteConfig, error) {
	route, err := database.GetRoute(ctx, idOrName)
	if err != nil {
		return network.RouteConfig{}, err
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	_, err = database.db.ExecContext(ctx, `UPDATE network_routes
SET consecutive_failures=0, cooldown_until=NULL, updated_at=? WHERE profile_id=? AND id=?`,
		updatedAt.UnixMilli(), database.profileID, route.ID)
	if err != nil {
		return network.RouteConfig{}, fmt.Errorf("reset route health: %w", err)
	}
	return database.GetRoute(ctx, route.ID)
}

type routeScanner interface {
	Scan(...any) error
}

func scanRoute(scanner routeScanner) (network.RouteConfig, error) {
	var route network.RouteConfig
	var authorizationRef string
	var classesJSON string
	var enabled bool
	var cooldownUntil sql.NullInt64
	var consecutiveFailures int
	var createdAt, updatedAt int64
	var success sql.NullBool
	var latency, statusCode, sampledAt, lastSuccessAt sql.NullInt64
	var errorClass sql.NullString
	err := scanner.Scan(
		&route.ID, &route.Name, &route.Kind, &route.Endpoint, &authorizationRef, &route.Trust, &classesJSON,
		&route.Priority, &enabled, &cooldownUntil, &consecutiveFailures, &createdAt, &updatedAt,
		&success, &latency, &statusCode, &errorClass, &sampledAt, &lastSuccessAt,
	)
	if err != nil {
		return network.RouteConfig{}, err
	}
	if err := json.Unmarshal([]byte(classesJSON), &route.Classes); err != nil {
		return network.RouteConfig{}, fmt.Errorf("decode route request classes: %w", err)
	}
	route.AuthorizationRef = authorizationRef
	route.AuthorizationConfigured = authorizationRef != ""
	route.Enabled = enabled
	route.CreatedAt = time.UnixMilli(createdAt)
	route.UpdatedAt = time.UnixMilli(updatedAt)
	route.ObservedAt = time.Now()
	route.Health = network.RouteHealth{
		ConsecutiveFailures: consecutiveFailures,
		CooldownUntil:       unixMillis(cooldownUntil),
		LastSampleAt:        unixMillis(sampledAt),
		LastSuccessAt:       unixMillis(lastSuccessAt),
		LastLatency:         time.Duration(latency.Int64) * time.Millisecond,
		LastStatusCode:      int(statusCode.Int64),
		LastErrorClass:      errorClass.String,
	}
	route.Health.State = routeHealthState(route, success)
	return route, nil
}

func routeHealthState(route network.RouteConfig, lastSuccess sql.NullBool) network.HealthState {
	if !route.Enabled {
		return network.HealthDisabled
	}
	if route.Health.CooldownUntil.After(route.ObservedAt) {
		return network.HealthCooldown
	}
	if !route.Health.CooldownUntil.IsZero() && !lastSuccess.Bool {
		return network.HealthRecovering
	}
	if !lastSuccess.Valid {
		return network.HealthUnknown
	}
	if lastSuccess.Bool {
		return network.HealthHealthy
	}
	return network.HealthDegraded
}

func durationMillis(value time.Duration) any {
	if value <= 0 {
		return nil
	}
	return value.Milliseconds()
}
