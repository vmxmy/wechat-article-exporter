package library

import (
	"context"
	"time"
)

// AnchorStat is one aggregated (surface, anchor) hit counter. The table is
// bounded by the number of anchor chains times their anchors — a handful of
// rows — so there is no retention concern, and it carries identifiers and
// counts only: never page content, URLs, or account names.
type AnchorStat struct {
	Surface   string
	Anchor    string
	HitCount  int64
	LastHitAt time.Time
}

// RecordAnchorHit upserts one hit. Callers treat this as best-effort
// observability: a failure here must never fail the parse that produced it.
func (database *Database) RecordAnchorHit(ctx context.Context, surface, anchor string) error {
	if surface == "" || anchor == "" {
		return nil
	}
	_, err := database.db.ExecContext(ctx, `INSERT INTO anchor_stats(surface, anchor, hit_count, last_hit_at)
VALUES(?, ?, 1, ?)
ON CONFLICT(surface, anchor) DO UPDATE SET hit_count = hit_count + 1, last_hit_at = excluded.last_hit_at`,
		surface, anchor, time.Now().UnixMilli())
	return err
}

// AnchorStats lists every counter ordered by surface then anchor name, for
// the diagnostics surface.
func (database *Database) AnchorStats(ctx context.Context) ([]AnchorStat, error) {
	rows, err := database.db.QueryContext(ctx,
		`SELECT surface, anchor, hit_count, last_hit_at FROM anchor_stats ORDER BY surface, anchor`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stats []AnchorStat
	for rows.Next() {
		var stat AnchorStat
		var lastHit int64
		if err := rows.Scan(&stat.Surface, &stat.Anchor, &stat.HitCount, &lastHit); err != nil {
			return nil, err
		}
		stat.LastHitAt = time.UnixMilli(lastHit)
		stats = append(stats, stat)
	}
	return stats, rows.Err()
}
