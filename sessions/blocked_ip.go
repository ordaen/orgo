package sessions

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/ordaen/orgo/types"
)

// BlockedIPs is the cached repository of the blocked addresses and networks, matched by its IsBlocked method.
var BlockedIPs = repo.RegisterCached(&blockedIPsStore{})

// blockedIPsStore caches the blocks in memory. IsBlocked matches the parsed networks of a snapshot, rebuilt after
// every write through the repository and by Reload, so it does not query the database. The blocks written by other
// processes are seen after Reload, which is called on connect too.
type blockedIPsStore struct {
	repo.Cached[*BlockedIP]

	// mu orders the snapshot rebuilds, so the last stored snapshot is built from the latest cache
	mu       sync.Mutex
	snapshot atomic.Pointer[[]blockEntry]
}

// blockEntry is a block with its parsed network.
type blockEntry struct {
	prefix netip.Prefix
	block  *BlockedIP
}

// Setup loads the blocks of the table into the cache and rebuilds the snapshot, see repo.Cached.Setup.
func (c *blockedIPsStore) Setup() error {
	if err := c.Cached.Setup(); err != nil {
		return err
	}
	c.rebuild()
	return nil
}

// Reload loads the blocks of the table again, it is called when they are changed by another process.
func (c *blockedIPsStore) Reload() error {
	return c.Setup()
}

// Create creates the block and adds it to the snapshot, see repo.Cached.Create.
func (c *blockedIPsStore) Create(m *BlockedIP) (*BlockedIP, error) {
	defer c.rebuild()
	return c.Cached.Create(m)
}

// Update updates the block and the snapshot, see repo.Cached.Update.
func (c *blockedIPsStore) Update(m *BlockedIP, fields ...string) (*BlockedIP, error) {
	defer c.rebuild()
	return c.Cached.Update(m, fields...)
}

// Delete deletes the block and removes it from the snapshot, see repo.Cached.Delete.
func (c *blockedIPsStore) Delete(m *BlockedIP) error {
	defer c.rebuild()
	return c.Cached.Delete(m)
}

// rebuild builds the snapshot from the cached blocks.
func (c *blockedIPsStore) rebuild() {
	c.mu.Lock()
	defer c.mu.Unlock()
	recs := c.FindManyCached(func(*BlockedIP) bool { return true })
	entries := make([]blockEntry, 0, len(recs))
	for _, rec := range recs {
		// the stored networks are valid, they are checked on write and by the cidr column
		if prefix, err := parsePrefix(rec.IP); err == nil {
			entries = append(entries, blockEntry{prefix: prefix, block: rec})
		}
	}
	c.snapshot.Store(&entries)
}

// entries returns the blocks of the snapshot, they must not be changed.
func (c *blockedIPsStore) entries() []blockEntry {
	if e := c.snapshot.Load(); e != nil {
		return *e
	}
	return nil
}

// List returns the addresses and networks of the active blocks.
func (c *blockedIPsStore) List() []string {
	now := time.Now()
	var ips []string
	for _, e := range c.entries() {
		if !e.block.Expired(now) {
			ips = append(ips, e.block.IP)
		}
	}
	return ips
}

// IsBlocked reports whether the address or network ip is in a blocked network. It returns the seconds left
// of the longest temporary block, or 0 when it is blocked permanently. The expired blocks of ip are deleted.
func (c *blockedIPsStore) IsBlocked(ip string) (int64, bool) {
	target, err := parsePrefix(ip)
	if err != nil {
		return 0, false
	}
	now := time.Now()
	var left int64
	blocked, permanent := false, false
	var expired []*BlockedIP
	// all matching blocks are checked, so the expired ones are deleted
	for _, e := range c.entries() {
		// like ip >>= target in PostgreSQL: the block network contains the target network
		if e.prefix.Bits() > target.Bits() || !e.prefix.Contains(target.Addr()) {
			continue
		}
		if e.block.Expired(now) {
			expired = append(expired, e.block)
			continue
		}
		blocked = true
		permanent = permanent || e.block.Period == 0
		left = max(left, e.block.SecondsLeft(now))
	}
	for _, b := range expired {
		c.Delete(b)
	}
	if permanent {
		// a permanent block outlasts the temporary ones
		return 0, true
	}
	return left, blocked
}

// DeleteExpired deletes the expired temporary blocks.
func (c *blockedIPsStore) DeleteExpired() error {
	now := time.Now()
	var errs []error
	for _, e := range c.entries() {
		if e.block.Expired(now) {
			errs = append(errs, c.Delete(e.block))
		}
	}
	return errors.Join(errs...)
}

// BlockIP adds IP address or network to block list, for temp seconds or permanently when temp is 0
func BlockIP(addr string, temp int, reason string) (*BlockedIP, error) {
	r := &BlockedIP{IP: addr, Period: temp, Reason: reason}
	return BlockedIPs.Create(r)
}

var (
	_ pg.BeforeCreateHook = (*BlockedIP)(nil)
	_ pg.BeforeUpdateHook = (*BlockedIP)(nil)
	_ pg.DBErrorHandler   = (*BlockedIP)(nil)
)

// BlockedIP model, a block of an address or a network for Period seconds, or permanently when Period is 0
type BlockedIP struct {
	model.Base[model.ID]
	IP     string `json:"ip"`
	Period int    `json:"period"`
	Reason string `json:"reason,omitempty"`
	types.UserFields
}

// TableName returns "blocked_ips".
func (m *BlockedIP) TableName() string {
	return "blocked_ips"
}

// SecondsLeft returns the seconds left of a temporary block at now, 0 for a permanent or expired block.
func (m *BlockedIP) SecondsLeft(now time.Time) int64 {
	if m.Period <= 0 {
		return 0
	}
	return max(0, int64(m.Period)-(now.Unix()-m.Created.Unix()))
}

// Expired reports whether the block is temporary and expired at now.
func (m *BlockedIP) Expired(now time.Time) bool {
	return m.Period > 0 && m.SecondsLeft(now) == 0
}

// BeforeCreate validates the address or network and stores it in canonical form.
func (m *BlockedIP) BeforeCreate(ctx context.Context, tx pg.Tx) error {
	return m.setIP()
}

// BeforeUpdate validates the address or network and stores it in canonical form.
func (m *BlockedIP) BeforeUpdate(ctx context.Context, tx pg.Tx) error {
	return m.setIP()
}

func (m *BlockedIP) setIP() error {
	var err error
	m.IP, err = convertIP(m.IP)
	return err
}

// convertIP validates the address or network ip and returns it in canonical form, a network without its host bits.
func convertIP(ip string) (string, error) {
	if ip == "" {
		return "", errors.New("address can't be blank")
	}
	if strings.Contains(ip, "/") {
		p, err := netip.ParsePrefix(ip)
		if err != nil {
			return "", fmt.Errorf("invalid network %q", ip)
		}
		return p.Masked().String(), nil
	}
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return "", fmt.Errorf("invalid address %q", ip)
	}
	return a.String(), nil
}

// parsePrefix parses the address or network ip as a network without its host bits,
// an address is a network of all its bits.
func parsePrefix(ip string) (netip.Prefix, error) {
	if strings.Contains(ip, "/") {
		p, err := netip.ParsePrefix(ip)
		if err != nil {
			return netip.Prefix{}, err
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a.WithZone(""), a.BitLen()), nil
}

// HandleDBError returns an error naming the address when it is already blocked.
func (b *BlockedIP) HandleDBError(op string, err *pg.PgError) error {
	if err.ConstraintName == "blocked_ips_ip_key" {
		return fmt.Errorf("ip: %s already exists", b.IP)
	}
	return nil // keep the database error
}
