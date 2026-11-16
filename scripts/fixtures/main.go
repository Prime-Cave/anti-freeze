// Command fixtures writes the synthetic sample recordings in testdata/.
// They are generated, never recorded from a real Mac.
package main

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/Prime-Cave/anti-freeze/internal/collector"
)

func main() {
	start := time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)
	write("testdata/slowdown.json", collector.SlowdownScenario(start))
	write("testdata/steady.json", collector.SteadyScenario(start, 240))
}

func write(path string, sc collector.Scenario) {
	b, err := json.Marshal(sc)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		log.Fatal(err)
	}
}
