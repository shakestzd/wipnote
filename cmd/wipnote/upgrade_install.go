package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractArchive extracts a .tar.gz archive into destRoot, preserving the
// archive's internal directory layout. Used to lift out both the top-level
// wipnote binary and the bundled plugin/ tree (since v0.59) in a single pass.
//
// Hardens against path-traversal entries ("../") by rejecting any name that
// escapes destRoot. Regular files inherit the tar header's mode (with 0o600
// minimum so we can still read what we write). Directories are created with
// 0o755. Other entry types (symlinks, devices) are ignored — we control the
// archive and don't produce them.
func extractArchive(tarball, destRoot string) error {
	f, err := os.Open(tarball)
	if err != nil {
		return err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("reading gzip: %w", err)
	}
	defer gr.Close()

	absRoot, err := filepath.Abs(destRoot)
	if err != nil {
		return fmt.Errorf("resolving extract root: %w", err)
	}

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		// Skip entries with absolute paths or "../" traversal.
		clean := filepath.Clean(hdr.Name)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
			continue
		}
		target := filepath.Join(absRoot, clean)
		// Defense in depth: confirm the resolved target is under destRoot.
		if !strings.HasPrefix(target+string(filepath.Separator), absRoot+string(filepath.Separator)) && target != absRoot {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkdir parent of %s: %w", target, err)
			}
			mode := os.FileMode(hdr.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
			if err != nil {
				return fmt.Errorf("creating %s: %w", target, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return fmt.Errorf("writing %s: %w", target, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("closing %s: %w", target, err)
			}
			if err := os.Chmod(target, mode); err != nil {
				return fmt.Errorf("chmod %s: %w", target, err)
			}
		default:
			// Symlinks/devices: not produced by goreleaser for this archive.
		}
	}
	return nil
}

// bundledShareTrees are the trees a release archive carries beside the
// binary, each installed to ~/.local/share/wipnote/<tree>.
var bundledShareTrees = []string{"plugin", "codex-marketplace", "antigravity-extension", "mods"}

// installShareTree replaces ~/.local/share/wipnote/<name> by moving srcDir
// into place after removing any prior contents, the same way the install
// scripts do, so upgrades via either path leave identical on-disk state.
func installShareTree(srcDir, name string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	metaDir := filepath.Join(home, ".local", "share", "wipnote")
	dest := filepath.Join(metaDir, name)

	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", metaDir, err)
	}
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("remove existing %s: %w", dest, err)
	}
	// Try rename first (atomic on same filesystem). Fall back to recursive
	// copy on cross-device — temp dirs and ~/.local/share can live on
	// different volumes (e.g. inside containers).
	if err := os.Rename(srcDir, dest); err == nil {
		return nil
	}
	return copyDirRecursive(srcDir, dest)
}

// copyDirRecursive walks src and copies every entry into dst, preserving file
// modes. Symlinks are skipped (the plugin tree doesn't contain any).
func copyDirRecursive(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode().IsRegular():
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		default:
			return nil
		}
	})
}

// atomicInstallBinary installs the binary at src to dest using the
// sibling-temp + rename pattern. A temp file is created in the same directory
// as dest (same filesystem), so os.Rename always succeeds without crossing a
// device boundary. Rename replaces the directory entry atomically: a running
// process keeps its old inode until exit, so this succeeds even when dest is a
// busy/executing binary (avoids ETXTBSY from in-place O_TRUNC writes).
func atomicInstallBinary(src, dest string) error {
	destDir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(destDir, ".wipnote-upgrade-*.tmp")
	if err != nil {
		return fmt.Errorf("creating sibling temp: %w", err)
	}
	tmpPath := tmp.Name()

	// Cleanup on any error so we never leave a .tmp turd behind.
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	in, err := os.Open(src)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("opening source binary: %w", err)
	}
	defer in.Close()

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp binary: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing temp binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp binary: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod temp binary: %w", err)
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("renaming temp to dest: %w", err)
	}
	success = true
	return nil
}

// atomicReplace replaces dest with src using atomicInstallBinary (sibling-temp
// + rename), which avoids ETXTBSY when dest is a currently-running executable.
func atomicReplace(src, dest string) error {
	return atomicInstallBinary(src, dest)
}

// checkWritable verifies the directory is writable by attempting to create a
// temp file inside it.
func checkWritable(dir string) error {
	tmp, err := os.CreateTemp(dir, ".wipnote-write-test-*")
	if err != nil {
		return err
	}
	tmp.Close()
	return os.Remove(tmp.Name())
}

// updateVersionFile writes the installed version to ~/.local/share/wipnote/.binary-version.
func updateVersionFile(ver string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	versionFile := filepath.Join(home, ".local", "share", "wipnote", ".binary-version")
	_ = os.MkdirAll(filepath.Dir(versionFile), 0o755)
	_ = os.WriteFile(versionFile, []byte(ver), 0o644)
}
