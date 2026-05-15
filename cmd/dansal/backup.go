package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
)

type backupResult struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Incremental bool   `json:"incremental"`
}

func adminBackup(req adminRequest) adminResponse {
	return createBackup(req.Path, time.Time{})
}

func adminIncrementalBackup(req adminRequest) adminResponse {
	if req.Since == "" {
		return adminResponse{OK: false, Error: "--since is required"}
	}
	since, err := time.Parse(time.RFC3339, req.Since)
	if err != nil {
		return adminResponse{OK: false, Error: "invalid since time: " + err.Error()}
	}
	return createBackup(req.Path, since)
}

func createBackup(outputPath string, since time.Time) adminResponse {
	incremental := !since.IsZero()

	if outputPath == "" {
		kind := "backup"
		if incremental {
			kind = "incremental"
		}
		outputPath = fmt.Sprintf("./dansal-%s-%s.tar.gz", kind, time.Now().Format("20060102-150405"))
	}

	// Consistent DB snapshot via VACUUM INTO a temp file.
	tmpDB, err := os.CreateTemp("", "dansal-db-*.db")
	if err != nil {
		return adminResponse{OK: false, Error: "temp file: " + err.Error()}
	}
	tmpDB.Close()
	defer os.Remove(tmpDB.Name())

	if _, err := db.Exec("VACUUM INTO ?", tmpDB.Name()); err != nil {
		return adminResponse{OK: false, Error: "db snapshot: " + err.Error()}
	}

	// Remove password hashes from the snapshot so plaintext backups never
	// contain credential data. Use a separate connection to the temp file.
	if snapDB, err := sql.Open("sqlite3", tmpDB.Name()); err == nil {
		snapDB.Exec("UPDATE users SET password_hash = ''")
		snapDB.Close()
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return adminResponse{OK: false, Error: "create archive: " + err.Error()}
	}

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	var archiveErr error

	// Config is always included — it is small and defines the runtime.
	if configFilePath != "" {
		archiveErr = addFileToTar(tw, configFilePath, "config.yaml")
	}

	// Database snapshot is always included.
	if archiveErr == nil {
		archiveErr = addFileToTar(tw, tmpDB.Name(), "calendar.db")
	}

	// Images — all for full backup, only changed files for incremental.
	if archiveErr == nil {
		archiveErr = addDirToTar(tw, config.Server.ImagesDir, "images", since)
	}

	tw.Close()
	gz.Close()
	f.Close()

	if archiveErr != nil {
		os.Remove(outputPath)
		return adminResponse{OK: false, Error: archiveErr.Error()}
	}

	info, _ := os.Stat(outputPath)
	var size int64
	if info != nil {
		size = info.Size()
	}

	return adminResponse{OK: true, Data: backupResult{
		Path:        outputPath,
		Size:        size,
		Incremental: incremental,
	}}
}

func addFileToTar(tw *tar.Writer, srcPath, name string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = name

	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

func adminRestore(req adminRequest) adminResponse {
	if req.Path == "" {
		return adminResponse{OK: false, Error: "path is required"}
	}
	restored, err := restoreFromTar(req.Path)
	if err != nil {
		return adminResponse{OK: false, Error: err.Error()}
	}
	if restored.Config && configFilePath != "" {
		reloadConfig(configFilePath)
	}
	return adminResponse{OK: true, Data: restored}
}

type restoreResult struct {
	Config bool `json:"config"`
	DB     bool `json:"db"`
	Images int  `json:"images"`
}

func restoreFromTar(tarPath string) (restoreResult, error) {
	var result restoreResult

	f, err := os.Open(tarPath)
	if err != nil {
		return result, err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return result, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	var dbRestorePath string

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, err
		}

		switch {
		case hdr.Name == "config.yaml":
			if configFilePath == "" {
				continue
			}
			if err := extractToFile(tr, configFilePath, hdr.FileInfo().Mode()); err != nil {
				return result, fmt.Errorf("restore config: %w", err)
			}
			result.Config = true

		case hdr.Name == "calendar.db":
			tmp, err := os.CreateTemp("", "dansal-restore-*.db")
			if err != nil {
				return result, fmt.Errorf("temp db: %w", err)
			}
			if _, err := io.Copy(tmp, tr); err != nil {
				tmp.Close()
				os.Remove(tmp.Name())
				return result, fmt.Errorf("extract db: %w", err)
			}
			tmp.Close()
			dbRestorePath = tmp.Name()

		case strings.HasPrefix(hdr.Name, "images/"):
			rel := strings.TrimPrefix(hdr.Name, "images/")
			if rel == "" || hdr.Typeflag == tar.TypeDir {
				continue
			}
			dest := filepath.Join(config.Server.ImagesDir, rel)
			if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
				return result, fmt.Errorf("mkdir for image: %w", err)
			}
			if err := extractToFile(tr, dest, hdr.FileInfo().Mode()); err != nil {
				return result, fmt.Errorf("restore image %s: %w", rel, err)
			}
			result.Images++
		}
	}

	if dbRestorePath != "" {
		defer os.Remove(dbRestorePath)
		if err := restoreDB(dbRestorePath); err != nil {
			return result, fmt.Errorf("restore db: %w", err)
		}
		result.DB = true
	}

	return result, nil
}

func extractToFile(r io.Reader, destPath string, mode os.FileMode) error {
	f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

// restoreDB uses SQLite's online backup API to replace the live database
// contents with those from srcPath without interrupting other connections.
func restoreDB(srcPath string) error {
	srcDB, err := sql.Open("sqlite3", srcPath)
	if err != nil {
		return err
	}
	defer srcDB.Close()

	srcConn, err := srcDB.Conn(context.Background())
	if err != nil {
		return err
	}
	defer srcConn.Close()

	destConn, err := db.Conn(context.Background())
	if err != nil {
		return err
	}
	defer destConn.Close()

	return srcConn.Raw(func(srcRaw any) error {
		src, ok := srcRaw.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("unexpected driver type")
		}
		return destConn.Raw(func(destRaw any) error {
			dst, ok := destRaw.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("unexpected driver type")
			}
			bk, err := dst.Backup("main", src, "main")
			if err != nil {
				return err
			}
			if _, err = bk.Step(-1); err != nil {
				return err
			}
			return bk.Finish()
		})
	})
}

func addDirToTar(tw *tar.Writer, srcDir, prefix string, since time.Time) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !since.IsZero() && !info.ModTime().After(since) {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		return addFileToTar(tw, path, filepath.Join(prefix, rel))
	})
}
