package cache

import (
	"fmt"
	"os"
	"play/internal/config"
	"play/internal/ui"
	"strings"
	"time"
)

// sizeBar renders a compact ASCII progress bar showing used/max.
//
//	[████████░░░░░░░░░░░░] 43%
func sizeBar(used, max int64, width int) string {
	if max <= 0 {
		return ""
	}
	ratio := float64(used) / float64(max)
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	empty := width - filled
	pct := int(ratio * 100)
	return fmt.Sprintf("[%s%s] %d%%",
		strings.Repeat("█", filled),
		strings.Repeat("░", empty),
		pct,
	)
}

func PrintStats() {
	if _, err := os.Stat(config.CacheDir); os.IsNotExist(err) {
		ui.SafePrintln("Cache is empty.")
		return
	}

	var trackCount, lockCount, staleLocks int
	var totalSize int64

	entries, _ := os.ReadDir(config.CacheDir)
	now := time.Now()

	for _, entry := range entries {
		name := entry.Name()
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if strings.HasSuffix(name, "."+config.AudioFormat) {
			trackCount++
			totalSize += info.Size()
		} else if strings.HasSuffix(name, ".lock") {
			lockCount++
			if now.Sub(info.ModTime()).Seconds() > config.LockStaleSeconds {
				staleLocks++
			}
		}
	}

	// Pull index stats from SQLite.
	var indexRows, uniqueVideos, totalHits int
	var oldestCached, newestCached int64

	d := getDB()
	d.QueryRow(`
		SELECT COUNT(*), COUNT(DISTINCT video_id), COALESCE(SUM(hit_count), 0),
		       COALESCE(MIN(cached_at), 0), COALESCE(MAX(cached_at), 0)
		FROM cache_index`,
	).Scan(&indexRows, &uniqueVideos, &totalHits, &oldestCached, &newestCached)

	usedMB := float64(totalSize) / (1024 * 1024)
	maxMB := float64(config.CacheMaxBytes) / (1024 * 1024)

	ui.SafePrintln("📊 Cache Stats:")
	ui.SafePrintf("  Cached tracks : %d\n", trackCount)
	ui.SafePrintf("  Size used     : %.1f MB / %.0f MB\n", usedMB, maxMB)
	ui.SafePrintf("  Capacity      : %s\n", sizeBar(totalSize, config.CacheMaxBytes, 20))
	ui.SafePrintf("  Active locks  : %d\n", lockCount-staleLocks)
	if staleLocks > 0 {
		ui.SafePrintf("  Stale locks   : %d\n", staleLocks)
	}
	ui.SafePrintln("")
	ui.SafePrintln("🗄  Index (SQLite):")
	ui.SafePrintf("  Index entries : %d\n", indexRows)
	ui.SafePrintf("  Unique videos : %d\n", uniqueVideos)
	ui.SafePrintf("  Total hits    : %d\n", totalHits)
	if oldestCached > 0 {
		ui.SafePrintf("  Oldest entry  : %s\n", time.Unix(oldestCached, 0).Format("2006-01-02"))
		ui.SafePrintf("  Newest entry  : %s\n", time.Unix(newestCached, 0).Format("2006-01-02"))
	}
}
