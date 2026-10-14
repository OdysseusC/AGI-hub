package app

import (
	"context"
	"log"

	"agihub/internal/driver"
	"agihub/internal/manager"
)

// A Client is independent per native session, but may share the sidecar process.
func RunClient(ctx context.Context, b *manager.Broker, binding Binding) {
	changes, cancel := b.Subscribe()
	defer cancel()
	for {
		for {
			if ctx.Err() != nil {
				return
			}
			message, err := b.Next(binding.Endpoint.ID)
			if err != nil {
				log.Printf("client %s: inbox unavailable", binding.Endpoint.ID)
				break
			}
			if message == nil {
				break
			}
			outcome, detail := driver.Send(ctx, binding.Driver, binding.Endpoint, *message)
			if err = b.Delivery(message.ID, outcome, detail); err != nil {
				// A native execution receipt can race ahead of the transport
				// response. Preserve its stronger evidence and keep this client alive.
				if b.Snapshot().Messages[message.ID].Delivery == "delivered" {
					continue
				}
				log.Printf("client %s: delivery receipt could not be saved", binding.Endpoint.ID)
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-changes:
		}
	}
}
