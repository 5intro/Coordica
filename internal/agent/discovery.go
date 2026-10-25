package agent

import (
	"context"
	"sync"
	"time"
)

// Give every registration its own deadline once a worker starts it. A stalled
// tool cannot consume the time available to a later healthy installation.
// Keep concurrency bounded and results in registration order.
func (r *Registry) Discover(ctx context.Context, executables map[string]string) []Capability {
	definitions := r.Definitions()
	caps := make([]Capability, len(definitions))
	jobs := make(chan int, len(definitions))
	for i := range definitions {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(4, len(definitions)) {
		workers.Go(func() {
			for i := range jobs {
				def := definitions[i]
				toolCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
				cap := r.Probe(toolCtx, def.ID, executables[def.ID])
				if cap.Available {
					cap.Models, cap.ModelDiscovery = r.discoverModels(toolCtx, def, cap)
					if cap.ModelDiscovery == "unavailable" {
						cap.Detail = "tool detected; model catalog unavailable; discovery incomplete"
					}
				}
				if toolCtx.Err() != nil {
					cap.Detail = "tool discovery interrupted; catalog incomplete"
				}
				cancel()
				caps[i] = cap
			}
		})
	}
	workers.Wait()
	return caps
}
