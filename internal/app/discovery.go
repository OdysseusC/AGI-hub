package app

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"

	"agihub/internal/discovery"
	"agihub/internal/mesh"
)

type discoveryHandle interface {
	Close() error
	Done() <-chan struct{}
	Err() error
}

type discoveryMesh interface {
	Announcement() mesh.Candidate
	Snapshot() mesh.State
	SetCandidates([]mesh.Candidate)
}

// DiscoveryController owns only discovery. Stopping it never reconfigures,
// disconnects, unpairs or otherwise modifies an established mesh connection.
type DiscoveryController struct {
	lifecycle  sync.Mutex
	updates    sync.Mutex
	mu         sync.RWMutex
	mesh       discoveryMesh
	onChange   func()
	start      func(context.Context, discovery.Options) (discoveryHandle, error)
	interfaces func() ([]discovery.NetworkInterface, error)

	handle       discoveryHandle
	generation   uint64
	enabled      bool
	iface        string
	lastErr      string
	announcement discovery.Advertisement
}

func NewDiscoveryController(service *mesh.Service, onChange func()) *DiscoveryController {
	var backend discoveryMesh
	if service != nil {
		backend = service
	}
	return newDiscoveryController(backend, onChange)
}

func newDiscoveryController(service discoveryMesh, onChange func()) *DiscoveryController {
	return &DiscoveryController{
		mesh: service, onChange: onChange, interfaces: discovery.Interfaces,
		start: func(ctx context.Context, options discovery.Options) (discoveryHandle, error) {
			return discovery.Start(ctx, options)
		},
	}
}

func (d *DiscoveryController) State() (enabled bool, iface string, lastErr string) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.enabled, d.iface, d.lastErr
}

// Configure retires the previous service before attempting changed settings.
// Any validation, interface or socket failure leaves discovery disabled.
func (d *DiscoveryController) Configure(ctx context.Context, enabled bool, iface string) error {
	d.lifecycle.Lock()
	defer d.lifecycle.Unlock()

	d.updates.Lock()
	d.mu.Lock()
	old := d.handle
	d.handle, d.enabled, d.iface, d.lastErr = nil, false, iface, ""
	d.generation++
	generation := d.generation
	d.mu.Unlock()
	if d.mesh != nil {
		d.mesh.SetCandidates(nil)
	}
	d.updates.Unlock()
	var closeErr error
	if old != nil {
		closeErr = old.Close()
	}
	if !enabled {
		if closeErr != nil {
			d.fail(closeErr)
		} else {
			d.changed()
		}
		return closeErr
	}
	if ctx == nil {
		return d.fail(errors.New("discovery requires a context"))
	}
	if err := ctx.Err(); err != nil {
		return d.fail(err)
	}
	if strings.TrimSpace(iface) == "" {
		return d.fail(errors.New("select a network interface before enabling discovery"))
	}
	if d.mesh == nil || !d.mesh.Snapshot().Config.Enabled {
		return d.fail(errors.New("enable LAN transport before enabling discovery"))
	}
	a := d.mesh.Announcement()
	address, err := d.advertisedAddress(a.Address, iface)
	if err != nil {
		return d.fail(err)
	}
	announcement := discovery.Advertisement{
		HubID: a.HubID, Name: a.Name, Address: address, Protocol: strconv.Itoa(a.Version),
	}
	handle, err := d.start(ctx, discovery.Options{
		Interface: iface, Announcement: announcement,
		OnChange: func(candidates []discovery.Candidate) {
			d.publish(generation, candidates)
		},
	})
	if err != nil {
		return d.fail(err)
	}
	if handle == nil {
		return d.fail(errors.New("discovery did not return a running service"))
	}
	d.mu.Lock()
	d.handle, d.enabled, d.announcement = handle, true, announcement
	d.mu.Unlock()
	go d.watch(generation, handle)
	d.changed()
	return nil
}

func (d *DiscoveryController) advertisedAddress(address, iface string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", errors.New("LAN listener has no usable advertised address")
	}
	ip := net.ParseIP(host)
	if host != "" && (ip == nil || !ip.IsUnspecified()) {
		return address, nil
	}
	interfaces, err := d.interfaces()
	if err != nil {
		return "", err
	}
	for _, item := range interfaces {
		if item.Name != iface {
			continue
		}
		for _, address := range item.Addresses {
			if ip := net.ParseIP(address); ip != nil && ip.To4() != nil {
				return net.JoinHostPort(ip.String(), port), nil
			}
		}
	}
	return "", errors.New("selected discovery interface has no IPv4 address")
}

func (d *DiscoveryController) publish(generation uint64, candidates []discovery.Candidate) {
	d.updates.Lock()
	defer d.updates.Unlock()
	d.mu.RLock()
	current := d.generation == generation
	d.mu.RUnlock()
	if !current || d.mesh == nil {
		return
	}
	mapped := make([]mesh.Candidate, 0, len(candidates))
	for _, c := range candidates {
		version, err := strconv.Atoi(c.Protocol)
		if err == nil && version == mesh.Version {
			mapped = append(mapped, mesh.Candidate{
				HubID: c.HubID, Name: c.Name, Address: c.Address, Version: version, ExpiresAt: c.ExpiresAt,
			})
		}
	}
	d.mesh.SetCandidates(mapped)
	d.changed()
}

func (d *DiscoveryController) watch(generation uint64, handle discoveryHandle) {
	<-handle.Done()
	d.updates.Lock()
	d.mu.Lock()
	if d.generation != generation {
		d.mu.Unlock()
		d.updates.Unlock()
		return
	}
	d.enabled, d.handle = false, nil
	d.generation++
	if err := handle.Err(); err != nil {
		d.lastErr = err.Error()
	}
	d.mu.Unlock()
	if d.mesh != nil {
		d.mesh.SetCandidates(nil)
	}
	d.updates.Unlock()
	d.changed()
}

func (d *DiscoveryController) fail(err error) error {
	d.mu.Lock()
	d.enabled, d.lastErr = false, err.Error()
	d.mu.Unlock()
	d.changed()
	return err
}

func (d *DiscoveryController) changed() {
	if d.onChange != nil {
		d.onChange()
	}
}

func (d *DiscoveryController) Close() error {
	_, iface, _ := d.State()
	return d.Configure(context.Background(), false, iface)
}
