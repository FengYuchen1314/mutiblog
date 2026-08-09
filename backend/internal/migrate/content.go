// Package migrate owns forward-only migrations for canonical content files.
package migrate

import (
	"context"
	"fmt"
	"time"

	"github.com/fengyuchen/mutiblog/internal/content"
)

// CurrentContentSchema is stored in config.yaml. Older installations are
// migrated forward; newer installations are rejected by the application.
const CurrentContentSchema = 2

type Report struct {
	From, To int
	Changed  []string
}

// Preview and Run share the same traversal so dry-run output is trustworthy.
// The first published content migration adds metadata.createdAt, using the
// earliest language-file timestamp for old bundles. It is idempotent because
// an existing createdAt is never overwritten.
func Run(ctx context.Context, store *content.Store, from, to int, dryRun bool) (Report, error) {
	report := Report{From: from, To: to}
	if from > CurrentContentSchema {
		return report, fmt.Errorf("content schema %d is newer than this binary (supports %d)", from, CurrentContentSchema)
	}
	if to == 0 || to > CurrentContentSchema {
		to = CurrentContentSchema
	}
	if from >= to {
		return report, nil
	}
	articles, scanErrors, err := store.ScanAll(ctx)
	if err != nil {
		return report, err
	}
	if len(scanErrors) > 0 {
		return report, fmt.Errorf("refusing migration while %d content bundles cannot be parsed", len(scanErrors))
	}
	if from < 2 && to >= 2 {
		for _, article := range articles {
			if ctx.Err() != nil {
				return report, ctx.Err()
			}
			if !article.CreatedAt.IsZero() {
				continue
			}
			var earliest time.Time
			for _, version := range article.Versions {
				if earliest.IsZero() || version.FileModTime.Before(earliest) {
					earliest = version.FileModTime
				}
			}
			if earliest.IsZero() {
				continue
			}
			article.CreatedAt = earliest.UTC()
			report.Changed = append(report.Changed, article.BundleDir+"/metadata.yaml: add createdAt")
			if !dryRun {
				if err := store.SaveMetadata(article); err != nil {
					return report, err
				}
			}
		}
	}
	return report, nil
}
