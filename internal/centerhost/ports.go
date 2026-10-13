package centerhost

import (
	"context"
	"fmt"
	"net"
)

func reservePorts(ctx context.Context, addresses []string) (func(), error) {
	var listeners []net.Listener
	release := func() {
		for _, ln := range listeners {
			ln.Close()
		}
	}
	seen := map[string]bool{}
	for _, address := range addresses {
		if seen[address] {
			continue
		}
		seen[address] = true
		var lc net.ListenConfig
		ln, e := lc.Listen(ctx, "tcp", address)
		if e != nil {
			release()
			return nil, fmt.Errorf("configured port unavailable (%s); existing service untouched: %w", address, e)
		}
		listeners = append(listeners, ln)
	}
	return release, nil
}
