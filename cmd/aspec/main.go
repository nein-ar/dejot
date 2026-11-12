package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nein-ar/dejot/aspec"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Name: "aspec",
		Usage: "aspec compiler and validator",
		Commands: []*cli.Command{
			{
				Name:  "validate",
				Usage: "validate an ASPEC file",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() == 0 {
						return fmt.Errorf("missing input file")
					}
					inputPath := cmd.Args().Get(0)
					_, _, err := aspec.Expand(inputPath)
					if err != nil {
						return err
					}
					fmt.Println("ASPEC validation successful")
					return nil
				},
			},
			{
				Name:  "compile",
				Usage: "compile an ASPEC file",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "output",
						Aliases: []string{"o"},
						Usage:   "output file",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() == 0 {
						return fmt.Errorf("missing input file")
					}
					inputPath := cmd.Args().Get(0)
					expanded, _, err := aspec.Expand(inputPath)
					if err != nil {
						return err
					}

					outputPath := cmd.String("output")
					if outputPath == "" {
						ext := filepath.Ext(inputPath)
						base := strings.TrimSuffix(inputPath, ext)
						dateStr := time.Now().Format("02012006")
						outputPath = base + "c" + dateStr + ext
					}

					return os.WriteFile(outputPath, expanded, 0644)
				},
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
