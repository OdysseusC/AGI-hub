package discovery

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxCacheRecords = 512

type cachedRecord struct {
	record
	seen, expires time.Time
}

type cache struct {
	self, iface string
	records     map[string]cachedRecord
}

func newCache(self, iface string) *cache {
	return &cache{self: self, iface: iface, records: make(map[string]cachedRecord)}
}

func recordKey(r record) string {
	if r.typ == typeTXT || r.typ == typeSRV {
		return fmt.Sprintf("%s/%d", r.name, r.typ)
	}
	return fmt.Sprintf("%s/%d/%s/%s", r.name, r.typ, r.target, r.ip.String())
}

func (c *cache) expire(now time.Time) {
	for key, r := range c.records {
		if !r.expires.After(now) {
			delete(c.records, key)
		}
	}
}

func (c *cache) apply(p packet, now time.Time) {
	if !p.response {
		return
	}
	c.expire(now)
	for _, r := range p.records {
		// Keep only local DNS-SD records; arbitrary unicast DNS cannot fill cache.
		if !strings.HasSuffix(r.name, ".local.") {
			continue
		}
		key := recordKey(r)
		old, exists := c.records[key]
		if r.ttl == 0 {
			// RFC 6762 section 10.1: goodbye records get a one-second grace.
			if exists && old.expires.After(now.Add(time.Second)) {
				old.expires = now.Add(time.Second)
				c.records[key] = old
			}
			continue
		}
		if !exists && len(c.records) >= maxCacheRecords {
			continue
		}
		if r.flush {
			for otherKey, other := range c.records {
				if otherKey != key && other.name == r.name && other.typ == r.typ && now.Sub(other.seen) >= time.Second {
					other.expires = now.Add(time.Second)
					c.records[otherKey] = other
				}
			}
		}
		ttl := r.ttl
		if ttl > recordTTL {
			ttl = recordTTL
		}
		c.records[key] = cachedRecord{record: r, seen: now, expires: now.Add(time.Duration(ttl) * time.Second)}
	}
}

func (c *cache) candidates(now time.Time) []Candidate {
	c.expire(now)
	out := make([]Candidate, 0)
	seen := make(map[string]bool)
	for _, ptr := range c.records {
		if ptr.typ != typePTR || ptr.name != ServiceName || !strings.HasSuffix(ptr.target, "."+ServiceName) {
			continue
		}
		srv, ok := c.records[recordKey(record{name: ptr.target, typ: typeSRV})]
		if !ok || srv.port == 0 || !strings.HasSuffix(srv.target, ".local.") {
			continue
		}
		txt, ok := c.records[recordKey(record{name: ptr.target, typ: typeTXT})]
		if !ok {
			continue
		}
		a := Advertisement{HubID: txt.txt["hub_id"], Name: txt.txt["name"], Protocol: txt.txt["protocol"]}
		if a.HubID == c.self || !validID(a.HubID) || !validText(a.Name, 128) || a.Protocol != ProtocolVersion {
			continue
		}
		// Prefer IPv4 deterministically when a peer publishes both families.
		var address *cachedRecord
		for _, rr := range c.records {
			if rr.name == srv.target && (rr.typ == typeA || rr.typ == typeAAAA) && usableIP(rr.ip) {
				r := rr
				if address == nil || r.typ < address.typ || r.typ == address.typ && r.ip.String() < address.ip.String() {
					address = &r
				}
			}
		}
		if address == nil || seen[a.HubID] {
			continue
		}
		seen[a.HubID] = true
		host := address.ip.String()
		if address.ip.To4() == nil && address.ip.IsLinkLocalUnicast() {
			host += "%" + c.iface
		}
		a.Address = net.JoinHostPort(host, strconv.Itoa(int(srv.port)))
		last, expiry := ptr.seen, ptr.expires
		for _, r := range []cachedRecord{srv, txt, *address} {
			if r.seen.After(last) {
				last = r.seen
			}
			if r.expires.Before(expiry) {
				expiry = r.expires
			}
		}
		out = append(out, Candidate{Advertisement: a, Interface: c.iface, LastSeen: last, ExpiresAt: expiry})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].HubID < out[j].HubID })
	return out
}
