package service

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

var (
	ErrNoNewExportFound = errors.New("no new letterboxd export zip found")
)

type LetterboxdDownloader struct{}

func NewLetterboxdDownloader() (*LetterboxdDownloader, error) {
	return &LetterboxdDownloader{}, nil
}

// DownloadLatestExport is browser-assisted:
// 1) opens Letterboxd export page in browser
// 2) waits for a newly downloaded zip in download dir
// 3) copies that zip into outDir and returns both import copy and source zip path
func (d *LetterboxdDownloader) DownloadLatestExport(ctx context.Context, _ LetterboxdCredentials, outDir string) (DownloadedExport, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return DownloadedExport{}, err
	}
	downloadDir, err := resolveDownloadDir()
	if err != nil {
		return DownloadedExport{}, err
	}
	before, err := snapshotZipFiles(downloadDir)
	if err != nil {
		return DownloadedExport{}, err
	}
	if err := openInBrowser("https://letterboxd.com/data/export/"); err != nil {
		return DownloadedExport{}, err
	}

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return DownloadedExport{}, ctx.Err()
		default:
		}
		zipPath, err := newestNewDiaryZip(downloadDir, before)
		if err == nil {
			out := filepath.Join(outDir, filepath.Base(zipPath))
			if err := copyFile(zipPath, out); err != nil {
				return DownloadedExport{}, err
			}
			return DownloadedExport{ImportPath: out, SourcePath: zipPath}, nil
		}
		time.Sleep(2 * time.Second)
	}
	return DownloadedExport{}, ErrNoNewExportFound
}

// ValidateCredentials opens browser login and relies on browser-native auth validation.
func (d *LetterboxdDownloader) ValidateCredentials(ctx context.Context, _ LetterboxdCredentials) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := openInBrowser("https://letterboxd.com/sign-in/"); err != nil {
		return err
	}
	return nil
}

func resolveDownloadDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("FILM_HEATMAP_DOWNLOAD_DIR")); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}

func snapshotZipFiles(dir string) (map[string]time.Time, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]time.Time{}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".zip") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		st, err := os.Stat(full)
		if err != nil {
			continue
		}
		out[full] = st.ModTime()
	}
	return out, nil
}

func newestNewDiaryZip(dir string, before map[string]time.Time) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	type candidate struct {
		path string
		mod  time.Time
	}
	list := make([]candidate, 0)
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".zip") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		st, err := os.Stat(full)
		if err != nil {
			continue
		}
		oldMod, existed := before[full]
		if existed && !st.ModTime().After(oldMod) {
			continue
		}
		hasDiary, err := hasDiaryCSV(full)
		if err != nil || !hasDiary {
			continue
		}
		list = append(list, candidate{path: full, mod: st.ModTime()})
	}
	if len(list) == 0 {
		return "", ErrNoNewExportFound
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].mod.After(list[j].mod)
	})
	return list[0].path, nil
}

func hasDiaryCSV(zipPath string) (bool, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return false, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.EqualFold(filepath.Base(f.Name), "diary.csv") {
			return true, nil
		}
	}
	return false, nil
}

func openInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("unsupported OS for browser auth: %s", runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
