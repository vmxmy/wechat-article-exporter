package app

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/migration"
)

func (a *App) migrationCommand() *cobra.Command {
	command := &cobra.Command{Use: "migration", Short: "Inspect, import, and verify legacy Web archives"}
	command.AddCommand(a.migrationInspectCommand(), a.migrationImportCommand(), a.migrationVerifyCommand())
	return command
}

func (a *App) migrationInspectCommand() *cobra.Command {
	return &cobra.Command{
		Use: "inspect <archive.zip>", Short: "Validate a legacy Web archive and show its staged import plan",
		Args: exactArgs(1, "migration inspect requires <archive.zip>"),
		RunE: func(command *cobra.Command, args []string) error {
			plan, err := migration.Plan(command.Context(), args[0], migration.PlanOptions{})
			if err != nil {
				return err
			}
			return a.output(map[string]any{"success": true, "data": map[string]any{
				"archive": map[string]any{"path": plan.Archive.Path, "fingerprint": plan.Archive.Fingerprint,
					"format": plan.Manifest.Format, "schemaVersion": plan.Manifest.SchemaVersion,
					"dexieSchemaVersion": plan.Manifest.Source.SchemaVersion(), "status": plan.Manifest.Status},
				"plan": plan.Report,
			}})
		},
	}
}

func (a *App) migrationImportCommand() *cobra.Command {
	var conflict, confirmation string
	command := &cobra.Command{
		Use: "import <archive.zip>", Short: "Validate and import a legacy Web archive into the active profile",
		Args: exactArgs(1, "migration import requires <archive.zip>"),
		RunE: func(command *cobra.Command, args []string) error {
			if a.active == nil || a.active.Library == nil || a.active.Objects == nil {
				return fmt.Errorf("active local profile storage is unavailable")
			}
			policy := migration.ConflictPolicy(strings.TrimSpace(conflict))
			if policy != migration.PreserveNewer && policy != migration.PreferArchive && policy != migration.RejectConflicts {
				return usage("--conflict must be preserve-newer, prefer-archive, or reject-conflicts")
			}
			required := "import-legacy:" + args[0]
			if confirmation != required {
				return usage("legacy import requires --confirm " + required)
			}
			plan, err := migration.Plan(command.Context(), args[0], migration.PlanOptions{})
			if err != nil {
				return err
			}
			target := migration.LocalTarget{Library: a.active.Library, Objects: a.active.Objects}
			report, err := migration.Import(command.Context(), plan, target, migration.ImportOptions{ConflictPolicy: policy})
			if err != nil {
				return err
			}
			verification, err := migration.Verify(command.Context(), plan, target)
			if err != nil {
				return err
			}
			return a.output(map[string]any{"success": verification.Success, "data": map[string]any{
				"profile": a.active.Profile.ID, "import": report, "verification": verification,
			}})
		},
	}
	command.Flags().StringVar(&conflict, "conflict", string(migration.PreserveNewer), "preserve-newer, prefer-archive, or reject-conflicts")
	command.Flags().StringVar(&confirmation, "confirm", "", "exact confirmation value import-legacy:<archive.zip>")
	return command
}

func (a *App) migrationVerifyCommand() *cobra.Command {
	return &cobra.Command{
		Use: "verify <archive.zip>", Short: "Compare a legacy archive with the active local library and object store",
		Args: exactArgs(1, "migration verify requires <archive.zip>"),
		RunE: func(command *cobra.Command, args []string) error {
			if a.active == nil || a.active.Library == nil || a.active.Objects == nil {
				return fmt.Errorf("active local profile storage is unavailable")
			}
			plan, err := migration.Plan(command.Context(), args[0], migration.PlanOptions{})
			if err != nil {
				return err
			}
			report, err := migration.Verify(command.Context(), plan, migration.LocalTarget{
				Library: a.active.Library, Objects: a.active.Objects,
			})
			if err != nil {
				return err
			}
			return a.output(map[string]any{"success": report.Success, "data": report})
		},
	}
}
