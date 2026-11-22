// Command probe prints what every provider returns, without the UI.
// Usage: go run ./cmd/probe
package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"kota/providers"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out []providers.Usage
	for _, p := range providers.All() {
		out = append(out, p.Fetch(ctx))
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}
