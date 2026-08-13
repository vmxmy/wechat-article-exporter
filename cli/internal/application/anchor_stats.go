package application

import (
	"context"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

// AnchorObservation is one recorded anchor's counters, annotated with its
// chain position so the reader sees fallback depth without knowing the chain.
type AnchorObservation struct {
	Anchor    string    `json:"anchor"`
	Position  int       `json:"position"`
	HitCount  int64     `json:"hitCount"`
	LastHitAt time.Time `json:"lastHitAt"`
}

// AnchorSurfaceDiagnostics reports one parsing surface. DriftSuspected is the
// point of the whole mechanism: the primary anchor going quiet while a
// fallback still resolves is the layout-drift signal that silent fallback
// chains otherwise swallow.
type AnchorSurfaceDiagnostics struct {
	Surface        string              `json:"surface"`
	DriftSuspected bool                `json:"driftSuspected"`
	Anchors        []AnchorObservation `json:"anchors"`
}

type anchorStatsProvider interface {
	AnchorStats(context.Context) ([]library.AnchorStat, error)
}

// AnchorDiagnostics joins recorded hit counters with the chain order declared
// by the wechat package. Surfaces with no recorded hits are omitted.
func (service *Service) AnchorDiagnostics(ctx context.Context) ([]AnchorSurfaceDiagnostics, error) {
	provider, ok := service.library.(anchorStatsProvider)
	if !ok {
		return nil, nil
	}
	stats, err := provider.AnchorStats(ctx)
	if err != nil {
		return nil, err
	}
	if len(stats) == 0 {
		return nil, nil
	}
	recorded := make(map[string]map[string]library.AnchorStat)
	for _, stat := range stats {
		if recorded[stat.Surface] == nil {
			recorded[stat.Surface] = make(map[string]library.AnchorStat)
		}
		recorded[stat.Surface][stat.Anchor] = stat
	}
	var result []AnchorSurfaceDiagnostics
	for _, surface := range wechat.AnchorSurfaces() {
		hits := recorded[surface.Surface]
		if len(hits) == 0 {
			continue
		}
		diagnostics := AnchorSurfaceDiagnostics{Surface: surface.Surface}
		var primary *library.AnchorStat
		fallbackLatest := time.Time{}
		for position, anchorName := range surface.Anchors {
			stat, ok := hits[anchorName]
			if position == 0 && ok {
				primaryCopy := stat
				primary = &primaryCopy
			}
			if position > 0 && ok && stat.LastHitAt.After(fallbackLatest) {
				fallbackLatest = stat.LastHitAt
			}
			if !ok {
				continue
			}
			diagnostics.Anchors = append(diagnostics.Anchors, AnchorObservation{
				Anchor: anchorName, Position: position + 1, HitCount: stat.HitCount, LastHitAt: stat.LastHitAt,
			})
		}
		if !fallbackLatest.IsZero() && (primary == nil || primary.LastHitAt.Before(fallbackLatest)) {
			diagnostics.DriftSuspected = true
		}
		result = append(result, diagnostics)
	}
	return result, nil
}
