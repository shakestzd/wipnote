package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"
	dbpkg "github.com/shakestzd/wipnote/core/db"
)

func runComplianceAutoBatch(ctx context.Context, flags complianceAutoFlags) error {
	since, err := time.Parse("2006-01-02", flags.batchSince)
	if err != nil {
		return fmt.Errorf("invalid --batch-since date %q: use format 2006-01-02", flags.batchSince)
	}

	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}

	database, err := openDB(wipnoteDir)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer database.Close()

	features, err := listDoneFeaturesSince(database, since)
	if err != nil {
		return fmt.Errorf("list done features: %w", err)
	}

	fmt.Printf("batch compliance: %d done features since %s\n", len(features), flags.batchSince)

	for i, feat := range features {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(30 * time.Second):
			}
		}
		fmt.Printf("[%d/%d] processing %s: %s\n", i+1, len(features), feat.ID, feat.Title)
		if err := runComplianceAuto(ctx, feat.ID, flags); err != nil {
			fmt.Fprintf(os.Stderr, "  error: %v\n", err)
		}
	}
	return nil
}

// listDoneFeaturesSince queries done features updated on or after since.
func listDoneFeaturesSince(database *sql.DB, since time.Time) ([]dbpkg.Feature, error) {
	rows, err := database.Query(`
		SELECT id, type, title, description, status, priority,
			assigned_to, track_id, created_at, updated_at, steps_total, steps_completed
		FROM features
		WHERE status = 'done' AND updated_at >= ?
		ORDER BY updated_at DESC`,
		since.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("query done features: %w", err)
	}
	defer rows.Close()

	var features []dbpkg.Feature
	for rows.Next() {
		var f dbpkg.Feature
		var trackID, assignedTo, description sql.NullString
		var createdStr, updatedStr string
		if err := rows.Scan(
			&f.ID, &f.Type, &f.Title, &description, &f.Status, &f.Priority,
			&assignedTo, &trackID, &createdStr, &updatedStr,
			&f.StepsTotal, &f.StepsCompleted,
		); err != nil {
			return nil, err
		}
		f.TrackID = trackID.String
		f.Description = description.String
		f.AssignedTo = assignedTo.String
		f.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		features = append(features, f)
	}
	return features, rows.Err()
}

// acquireComplianceLock takes an exclusive, non-blocking flock on lockPath and
// records the holder's PID in it for diagnostics. It returns a release function,
// or an error when another live process holds the lock. The kernel drops the
// flock on process death, so a crashed holder never leaves a stale lock, and a
// reused PID in the file means nothing. release never removes the file:
// deleting a flock file lets a second process lock a new inode while the first
// still holds the old one.
func acquireComplianceLock(lockPath string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create locks dir: %w", err)
	}
	lock := flock.New(lockPath)
	ok, err := lock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", filepath.Base(lockPath), err)
	}
	if !ok {
		return nil, fmt.Errorf("compliance already running for %s (%s)", filepath.Base(lockPath), holderPID(lockPath))
	}
	_ = os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
	return func() { _ = lock.Unlock() }, nil
}

// holderPID reads the PID recorded by the lock holder, for error messages.
func holderPID(lockPath string) string {
	data, err := os.ReadFile(lockPath)
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return "pid unknown"
	}
	return "pid " + strings.TrimSpace(string(data))
}
