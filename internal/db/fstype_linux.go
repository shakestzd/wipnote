//go:build linux

package db

import (
	"log"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Filesystem magic numbers for filesystems where WAL-SHM mmap is unsafe.
const (
	fuseSuperMagic      = 0x65735546
	v9fsMagic           = 0x01021997 // 9p / virtiofs on older kernels
	overlayFSSuperMagic = 0x794c7630
	nfsSuperMagic       = 0x6969
	smbSuperMagic       = 0xFE534D42
	smb2SuperMagic      = 0x517B

	// Safelisted native filesystem magic numbers (WAL is safe).
	ext4Magic  = 0xEF53
	xfsMagic   = 0x58465342
	btrfsMagic = 0x9123683E
	tmpfsMagic = 0x01021994
	zfsMagic   = 0x2FC12FC1
)

// isUnsafeForMmap returns true if the filesystem backing dbPath is known to be
// unsafe for SQLite WAL-mode mmap (e.g. virtiofs, FUSE, 9p, overlayfs, NFS,
// SMB). Returns false on safelisted native filesystems (ext4, xfs, btrfs,
// tmpfs, zfs). Unknown filesystems return true — prefer correctness over speed.
func isUnsafeForMmap(dbPath string) bool {
	dir := filepath.Dir(dbPath)
	var buf unix.Statfs_t
	if err := unix.Statfs(dir, &buf); err != nil {
		log.Printf("debug: fstype: statfs(%q) failed: %v — assuming unsafe", dir, err)
		return true
	}

	fsType := int64(buf.Type)
	switch fsType {
	case fuseSuperMagic, v9fsMagic, overlayFSSuperMagic, nfsSuperMagic, smbSuperMagic, smb2SuperMagic:
		return true
	case ext4Magic, xfsMagic, btrfsMagic, tmpfsMagic, zfsMagic:
		return false
	default:
		log.Printf("debug: fstype: unknown magic 0x%X for %q — assuming unsafe (add to safelist if WAL works)", buf.Type, dir)
		return true
	}
}
