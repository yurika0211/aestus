package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/yurika0211/aestus/memory"
)

func main() {
	dir := os.Getenv("AESTUS_DIR")
	if dir == "" {
		dir = "./.aestus/memory"
	}

	store, err := memory.NewStore(dir)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	written, err := store.Remember(ctx, memory.RememberRequest{
		Content:  "Aestus stores durable memories as Obsidian-compatible Markdown.",
		Category: "project", Tier: memory.TierLong, Importance: .9,
		Options: memory.SaveOptions{Tags: []string{"aestus", "memory"}, Links: []string{"Obsidian"}, Aliases: []string{"file-first memory"}},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("vault: %s; saved: %s\n", store.Dir(), written.ID)
	results, err := store.Recall(ctx, memory.RecallRequest{Query: "Obsidian memory", Options: memory.SearchOptions{Limit: 5, IncludeGraph: true, GraphDepth: 1, SkipAccessStats: true}})
	if err != nil {
		log.Fatal(err)
	}
	for _, result := range results {
		entry := result.Entry
		fmt.Printf("- [%s/%s] %s\n", entry.Category, entry.Tier, entry.Content)
	}
}
