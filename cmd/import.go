package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	importAll   bool
	forceImport bool
)

var importCmd = &cobra.Command{
	Use:   "import <archive_path> [target_profile_name]",
	Short: "Import a profile (or all profiles) from a gzipped tar archive",
	Long:  `Restores a profile directory from a compressed .tar.gz archive. If target_profile_name is omitted, it is inferred from the archive name. Use --all to import all profiles.`,
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		archivePath := args[0]

		file, err := os.Open(archivePath)
		if err != nil {
			return fmt.Errorf("failed to open archive: %w", err)
		}
		defer file.Close()

		if importAll {
			if len(args) > 1 {
				return fmt.Errorf("cannot specify a target profile name when importing all profiles (--all)")
			}

			if err := profile.ImportAll(file, forceImport); err != nil {
				return err
			}

			fmt.Println("Successfully imported all profiles.")
			return nil
		}

		var targetName string
		if len(args) == 2 {
			targetName = args[1]
		} else {
			base := filepath.Base(archivePath)
			if strings.HasSuffix(strings.ToLower(base), ".tar.gz") {
				targetName = base[:len(base)-7]
			} else if strings.HasSuffix(strings.ToLower(base), ".tgz") {
				targetName = base[:len(base)-4]
			} else {
				ext := filepath.Ext(base)
				targetName = base[:len(base)-len(ext)]
			}
		}

		if err := profile.ImportProfile(file, targetName, forceImport); err != nil {
			return err
		}

		fmt.Printf("Successfully imported profile as %q.\n", targetName)
		return nil
	},
}

func init() {
	importCmd.Flags().BoolVarP(&importAll, "all", "a", false, "Import all profiles from the archive")
	importCmd.Flags().BoolVarP(&forceImport, "force", "f", false, "Overwrite existing profiles during import")
	rootCmd.AddCommand(importCmd)
}
