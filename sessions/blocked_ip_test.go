package sessions

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expireBlock moves the creation time of the block back by ago.
func expireBlock(t *testing.T, id model.ID, ago time.Duration) {
	_, err := pg.DB.Exec(t.Context(), `UPDATE blocked_ips SET created = created - $2 * interval '1 second' WHERE id = $1`, id, int(ago.Seconds()))
	require.NoError(t, err)
	// the cache is loaded again with the changed record
	require.NoError(t, BlockedIPs.Setup())
}

func TestModelBlock(t *testing.T) {
	clearTables(t, "blocked_ips")
	r := &BlockedIP{IP: "1.1.1.1/24"}
	assert.Equal(t, "blocked_ips", r.TableName())
	ip, err := BlockedIPs.Create(r)
	require.NoError(t, err)
	assert.Equal(t, "1.1.1.0/24", ip.IP)
}

func TestConvertIP(t *testing.T) {
	for in, want := range map[string]string{
		"1.1.1.1":        "1.1.1.1",
		"1.1.1.1/28":     "1.1.1.0/28",
		"2001:DB8::1":    "2001:db8::1",
		"2001:db8::1/32": "2001:db8::/32",
	} {
		got, err := convertIP(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"a", "1.1.1", "1.1.1.1/33", "a/24"} {
		_, err := convertIP(in)
		assert.Error(t, err, in)
	}
	_, err := convertIP("")
	assert.EqualError(t, err, "address can't be blank")
}

func TestBlockIP(t *testing.T) {
	clearTables(t, "blocked_ips")
	_, err := BlockIP("", 100, "test")
	assert.EqualError(t, err, "address can't be blank")
	_, err = BlockIP("a", 100, "test")
	assert.EqualError(t, err, `invalid address "a"`)
	block, err := BlockIP("1.1.1.1/28", 100, "test")
	require.NoError(t, err)
	assert.Equal(t, "1.1.1.0/28", block.IP)
	assert.Equal(t, 100, block.Period)

	left, ok := BlockedIPs.IsBlocked("1.1.1.1")
	assert.True(t, ok)
	assert.InDelta(t, 100, left, 2)

	_, ok = BlockedIPs.IsBlocked("1.1.1.0/29")
	assert.True(t, ok)

	_, ok = BlockedIPs.IsBlocked("1.1.1.0/27")
	assert.False(t, ok)
	_, ok = BlockedIPs.IsBlocked("1.1.1.100/32")
	assert.False(t, ok)
	_, ok = BlockedIPs.IsBlocked("not an ip")
	assert.False(t, ok)
	assert.Equal(t, []string{"1.1.1.0/28"}, BlockedIPs.List())
}

// TestIsBlockedTimeLeft checks the seconds left are returned, not the period.
func TestIsBlockedTimeLeft(t *testing.T) {
	clearTables(t, "blocked_ips")
	block, err := BlockIP("1.2.3.4", 100, "test")
	require.NoError(t, err)
	expireBlock(t, block.ID, 30*time.Second)
	left, ok := BlockedIPs.IsBlocked("1.2.3.4")
	assert.True(t, ok)
	assert.InDelta(t, 70, left, 2)
}

func TestIsBlockedExpired(t *testing.T) {
	clearTables(t, "blocked_ips")
	block, err := BlockIP("1.2.3.4", 10, "test")
	require.NoError(t, err)
	expireBlock(t, block.ID, time.Minute)
	assert.Empty(t, BlockedIPs.List())

	_, ok := BlockedIPs.IsBlocked("1.2.3.4")
	assert.False(t, ok)
	assert.Zero(t, BlockedIPs.Count(), "the expired block is deleted")
}

// TestIsBlockedNested checks an expired block does not hide a permanent block of a larger network.
func TestIsBlockedNested(t *testing.T) {
	clearTables(t, "blocked_ips")
	tmp, err := BlockIP("10.1.0.0/16", 10, "temporary")
	require.NoError(t, err)
	_, err = BlockIP("10.0.0.0/8", 0, "permanent")
	require.NoError(t, err)
	expireBlock(t, tmp.ID, time.Hour)

	left, ok := BlockedIPs.IsBlocked("10.1.2.3")
	assert.True(t, ok)
	assert.Zero(t, left, "permanent")
	assert.Equal(t, 1, BlockedIPs.Count())
}

func TestIsBlockedLongest(t *testing.T) {
	clearTables(t, "blocked_ips")
	_, err := BlockIP("10.1.0.0/16", 100, "short")
	require.NoError(t, err)
	_, err = BlockIP("10.0.0.0/8", 1000, "long")
	require.NoError(t, err)
	left, ok := BlockedIPs.IsBlocked("10.1.2.3")
	assert.True(t, ok)
	assert.InDelta(t, 1000, left, 2)
}

func TestBlockedIPsDeleteExpired(t *testing.T) {
	clearTables(t, "blocked_ips")
	expired, err := BlockIP("1.1.1.1", 10, "expired")
	require.NoError(t, err)
	_, err = BlockIP("2.2.2.2", 100, "active")
	require.NoError(t, err)
	_, err = BlockIP("3.3.3.3", 0, "permanent")
	require.NoError(t, err)
	expireBlock(t, expired.ID, time.Minute)

	require.NoError(t, BlockedIPs.DeleteExpired())
	assert.ElementsMatch(t, []string{"2.2.2.2/32", "3.3.3.3/32"}, BlockedIPs.List())
	assert.Equal(t, 2, BlockedIPs.Count())
}

func TestBlockUniqueIP(t *testing.T) {
	clearTables(t, "blocked_ips")
	_, err := BlockIP("1.1.1.1", 100, "test")
	assert.NoError(t, err)
	_, err = BlockIP("1.1.1.1", 100, "test")
	assert.EqualError(t, err, "ip: 1.1.1.1 already exists")
}

// TestBlockedIPsReload checks the blocks written by another process are seen after Reload.
func TestBlockedIPsReload(t *testing.T) {
	clearTables(t, "blocked_ips")
	kept, err := BlockIP("1.1.1.1", 0, "kept")
	require.NoError(t, err)

	// written by another process, the cache does not see them
	_, err = pg.DB.Exec(t.Context(), `INSERT INTO blocked_ips (ip, period) VALUES ('10.0.0.0/8', 0)`)
	require.NoError(t, err)
	_, err = pg.DB.Exec(t.Context(), `DELETE FROM blocked_ips WHERE id = $1`, kept.ID)
	require.NoError(t, err)
	_, ok := BlockedIPs.IsBlocked("10.1.2.3")
	assert.False(t, ok)
	_, ok = BlockedIPs.IsBlocked("1.1.1.1")
	assert.True(t, ok)

	require.NoError(t, BlockedIPs.Reload())
	_, ok = BlockedIPs.IsBlocked("10.1.2.3")
	assert.True(t, ok)
	_, ok = BlockedIPs.IsBlocked("1.1.1.1")
	assert.False(t, ok)
	assert.Equal(t, []string{"10.0.0.0/8"}, BlockedIPs.List())
}

// TestBlockedIPsWrites checks the writes through the repository update the snapshot without Reload.
func TestBlockedIPsWrites(t *testing.T) {
	clearTables(t, "blocked_ips")
	block, err := BlockedIPs.Create(&BlockedIP{IP: "192.168.0.0/16"})
	require.NoError(t, err)
	_, ok := BlockedIPs.IsBlocked("192.168.1.1")
	assert.True(t, ok)

	block.IP = "172.16.0.0/12"
	block, err = BlockedIPs.Update(block, "ip")
	require.NoError(t, err)
	_, ok = BlockedIPs.IsBlocked("192.168.1.1")
	assert.False(t, ok)
	_, ok = BlockedIPs.IsBlocked("172.16.5.5")
	assert.True(t, ok)

	require.NoError(t, BlockedIPs.Delete(block))
	_, ok = BlockedIPs.IsBlocked("172.16.5.5")
	assert.False(t, ok)
	assert.Empty(t, BlockedIPs.List())
}

func TestIsBlockedIPv6(t *testing.T) {
	clearTables(t, "blocked_ips")
	_, err := BlockIP("2001:db8::/32", 0, "v6")
	require.NoError(t, err)
	_, ok := BlockedIPs.IsBlocked("2001:db8:1::5")
	assert.True(t, ok)
	_, ok = BlockedIPs.IsBlocked("2001:db9::1")
	assert.False(t, ok)
	_, ok = BlockedIPs.IsBlocked("32.1.13.184")
	assert.False(t, ok, "an IPv4 address is not in an IPv6 network")
}

func TestBlockedIPsConcurrent(t *testing.T) {
	clearTables(t, "blocked_ips")
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			for j := range 10 {
				_, err := BlockIP(fmt.Sprintf("10.%d.%d.0/24", i, j), 0, "c")
				assert.NoError(t, err)
				_, ok := BlockedIPs.IsBlocked(fmt.Sprintf("10.%d.%d.1", i, j))
				assert.True(t, ok)
				BlockedIPs.List()
			}
		})
	}
	wg.Go(func() {
		for range 5 {
			assert.NoError(t, BlockedIPs.Reload())
		}
	})
	wg.Wait()
	assert.Len(t, BlockedIPs.List(), 40)
}

func BenchmarkIsBlocked(b *testing.B) {
	if err := pg.ClearTables("blocked_ips"); err != nil {
		b.Fatal(err)
	}
	for i := range 200 {
		if _, err := BlockIP(fmt.Sprintf("10.%d.%d.0/24", i/250, i%250), 0, "bench"); err != nil {
			b.Fatal(err)
		}
	}
	for b.Loop() {
		BlockedIPs.IsBlocked("10.0.199.7")
	}
}
