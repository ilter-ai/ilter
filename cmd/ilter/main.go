package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ilter-ai/ilter/internal/app"
	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/provider"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "ilter",
		Short: "ILTER is an AI proxy gateway with caching, budget enforcement, and semantic routing.",
	}

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP proxy server",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := config.Load()
			defer config.WarnUnknownEnv()()
			application, err := app.New(cfg)
			if err != nil {
				return err
			}
			return application.RunServe()
		},
	}

	initCmd := newInitCmd()

	var updateModels bool
	modelsCmd := &cobra.Command{
		Use:   "models",
		Short: "List or update supported models and configurations",
		RunE: func(_ *cobra.Command, _ []string) error {
			if updateModels {
				cfg := config.Load()
				reg := provider.NewRegistry()
				if errInit := reg.InitFromConfig(cfg); errInit != nil {
					return fmt.Errorf("failed to initialize providers: %w", errInit)
				}

				fmt.Println("Discovering models from all configured providers...")
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()

				discoveredCount := 0
				for _, pCfg := range cfg.Providers {
					p, errGet := reg.Get(pCfg.Name)
					if errGet != nil {
						continue
					}
					discoveredModels, errDisc := p.DiscoverModels(ctx)
					if errDisc != nil {
						fmt.Printf("Warning: failed to discover models for provider %s: %v\n", pCfg.Name, errDisc)
						continue
					}
					for _, info := range discoveredModels {
						catalog.ModelsMu.Lock()
						catalog.Models[info.ID] = append(catalog.Models[info.ID], info)
						discoveredCount++
						catalog.ModelsMu.Unlock()
					}
				}
				fmt.Printf("Discovered %d new models. Run `ilter serve` to persist them to the database.\n", discoveredCount)
				return nil
			}

			catalog.ModelsMu.RLock()
			ids := make([]string, 0, len(catalog.Models))
			for id := range catalog.Models {
				ids = append(ids, id)
			}
			sort.Strings(ids)

			fmt.Printf("%-35s %-12s %-10s %-12s %-18s %-18s\n", "MODEL ID", "PROVIDER", "TIER", "MAX CONTEXT", "INPUT COST / 1M", "OUTPUT COST / 1M")
			fmt.Println(strings.Repeat("-", 111))
			for _, id := range ids {
				for _, m := range catalog.Models[id] {
					inputCostStr := formatCost(m.CostPerInputToken)
					outputCostStr := formatCost(m.CostPerOutputToken)
					fmt.Printf("%-35s %-12s %-10s %-12d %-18s %-18s\n",
						m.ID, m.Provider, m.Tier, m.MaxContextTokens, inputCostStr, outputCostStr)
				}
			}
			catalog.ModelsMu.RUnlock()
			return nil
		},
	}
	modelsCmd.Flags().BoolVarP(&updateModels, "update", "u", false, "Discover models from all configured providers")

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(modelsCmd)

	if err := rootCmd.Execute(); err != nil {
		slog.Error("Command execution failed", "error", err)
		os.Exit(1)
	}
}

func formatCost(cost float64) string {
	if cost == 0 {
		return "Free"
	}
	return fmt.Sprintf("$%.2f", cost*1000000)
}
