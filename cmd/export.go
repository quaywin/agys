package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	exportAll  bool
	outputFile string
)

var exportCmd = &cobra.Command{
	Use:               "export [profile_name]",
	Short:             "Export a profile (or all profiles) to a gzipped tar archive",
	Long:              `Packages all profile configuration and credentials into a compressed .tar.gz archive. Use the --all flag to export all profiles.`,
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if exportAll {
			if len(args) > 0 {
				return fmt.Errorf("cannot specify a profile name when exporting all profiles (--all)")
			}

			outPath := outputFile
			if outPath == "" {
				outPath = "agys_profiles_backup.tar.gz"
			}

			parentDir := filepath.Dir(outPath)
			if parentDir != "" && parentDir != "." {
				if err := os.MkdirAll(parentDir, 0755); err != nil {
					return fmt.Errorf("failed to create output directory: %w", err)
				}
			}

			file, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return fmt.Errorf("failed to create export file: %w", err)
			}
			defer file.Close()

			if err := profile.ExportAll(file); err != nil {
				_ = os.Remove(outPath)
				return err
			}

			absPath, err := filepath.Abs(outPath)
			if err != nil {
				absPath = outPath
			}
			fmt.Printf("Successfully exported all profiles to %s\n", absPath)
			return nil
		}

		if len(args) == 0 {
			return fmt.Errorf("must specify a profile name to export, or use --all")
		}

		profileName := args[0]

		exists, _, err := profile.Exists(profileName)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("profile %q does not exist", profileName)
		}

		outPath := outputFile
		if outPath == "" {
			outPath = profileName + ".tar.gz"
		}

		parentDir := filepath.Dir(outPath)
		if parentDir != "" && parentDir != "." {
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				return fmt.Errorf("failed to create output directory: %w", err)
			}
		}

		file, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return fmt.Errorf("failed to create export file: %w", err)
		}
		defer file.Close()

		if err := profile.ExportProfile(profileName, file); err != nil {
			_ = os.Remove(outPath)
			return err
		}

		absPath, err := filepath.Abs(outPath)
		if err != nil {
			absPath = outPath
		}
		fmt.Printf("Successfully exported profile %q to %s\n", profileName, absPath)
		return nil
	},
}

func init() {
	exportCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Path to the output archive file")
	exportCmd.Flags().BoolVarP(&exportAll, "all", "a", false, "Export all profiles into a single archive")
	rootCmd.AddCommand(exportCmd)
}
