package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	load "github.com/1XP-Inc/muhan-mud/server/tests/load"
)

func main() {
	mode := flag.String("mode", string(load.ModeRESTLike), "rest-like or persistent")
	target := flag.Int("target", 100, "safe staged target: 100, 250, 500, or 1000")
	workers := flag.Int("workers", 0, "local workers (default 32; maximum 32)")
	line := flag.String("line", "점수", "deterministic synthetic command line")
	timeout := flag.Duration("timeout", 30*time.Second, "local run timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := load.Run(ctx, load.Config{Mode: load.Mode(*mode), Target: *target, Workers: *workers, Line: *line})
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if encodeErr := encoder.Encode(report); encodeErr != nil {
		fmt.Fprintln(os.Stderr, encodeErr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
