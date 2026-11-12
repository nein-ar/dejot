// Command dejot-ssg builds a static site from Djot sources.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/nein-ar/dejot/ssg"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Name:  "dejot-ssg",
		Usage: "build a static site from Djot",
		Commands: []*cli.Command{
			{
				Name:  "build",
				Usage: "build the site",
				Flags: configFlags(true),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfg, err := configFromFlags(cmd)
					if err != nil {
						return err
					}
					site, err := ssg.Load(cfg)
					if err != nil {
						return err
					}
					res, err := site.BuildWithOptions(ssg.BuildOptions{Force: cmd.Bool("force"), DryRun: cmd.Bool("dry-run")})
					if err != nil {
						return err
					}
					report(res)
					return nil
				},
			},
			{
				Name:  "serve",
				Usage: "build and serve the site",
				Flags: append(configFlags(true), &cli.StringFlag{Name: "addr", Value: ":1313", Usage: "listen address"}),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfg, err := configFromFlags(cmd)
					if err != nil {
						return err
					}
					site, err := ssg.Load(cfg)
					if err != nil {
						return err
					}
					res, err := site.BuildWithOptions(ssg.BuildOptions{Force: cmd.Bool("force")})
					if err != nil {
						return err
					}
					report(res)

					log.Printf("serving %s on %s", cfg.OutputDir, cmd.String("addr"))
					srv := &http.Server{Addr: cmd.String("addr"), Handler: http.FileServer(http.Dir(cfg.OutputDir))}
					go func() {
						if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
							log.Printf("server: %v", err)
						}
					}()
					<-ctx.Done()
					return srv.Shutdown(context.Background())
				},
			},
			{
				Name:      "render",
				Usage:     "render one Djot file to HTML on stdout",
				ArgsUsage: "FILE",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() == 0 {
						return fmt.Errorf("missing input file")
					}
					html, err := ssg.RenderFile(cmd.Args().Get(0))
					if err != nil {
						return err
					}
					_, err = fmt.Print(html)
					return err
				},
			},
		},
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := cmd.Run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func configFlags(force bool) []cli.Flag {
	flags := []cli.Flag{
		&cli.StringFlag{Name: "config", Aliases: []string{"c"}, Value: "ssg.toml", Usage: "site configuration"},
		&cli.StringFlag{Name: "theme", Usage: "override theme"},
		&cli.StringFlag{Name: "content", Usage: "override content directory"},
		&cli.StringFlag{Name: "data", Usage: "override data directory"},
		&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "override output directory"},
		&cli.StringFlag{Name: "base-url", Usage: "override base URL"},
	}
	if force {
		flags = append(flags, &cli.BoolFlag{Name: "force", Usage: "ignore the build cache"})
	}
	return flags
}

func configFromFlags(cmd *cli.Command) (*ssg.Config, error) {
	var cfg *ssg.Config
	path := cmd.String("config")
	if _, err := os.Stat(path); err == nil {
		loaded, err := ssg.LoadConfig(path)
		if err != nil {
			return nil, err
		}
		cfg = loaded
	} else {
		cfg = ssg.NewConfig()
	}
	if v := cmd.String("theme"); v != "" {
		cfg.Theme = v
	}
	if v := cmd.String("content"); v != "" {
		cfg.ContentDir = v
	}
	if v := cmd.String("data"); v != "" {
		cfg.DataDir = v
	}
	if v := cmd.String("output"); v != "" {
		cfg.OutputDir = v
	}
	if v := cmd.String("base-url"); v != "" {
		cfg.BaseURL = v
	}
	return cfg, nil
}

func report(res ssg.BuildResult) {
	if res.Skipped {
		fmt.Printf("up to date (%s)\n", res.Duration.Round(1e6))
		return
	}
	fmt.Printf("%d pages, %d written, %d removed (%s)\n", res.Pages, res.Written, res.Removed, res.Duration.Round(1e6))
}
