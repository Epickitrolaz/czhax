package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Entry struct {
	Name    string
	Rel     string
	Path    string
	Dir     bool
	Size    int64
	Mode    os.FileMode
	ModTime int64
}

var textExt = map[string]bool{
	".txt": true, ".md": true, ".json": true, ".yaml": true, ".yml": true,
	".toml": true, ".ini": true, ".cfg": true, ".conf": true, ".log": true,
	".csv": true, ".xml": true, ".html": true, ".css": true, ".js": true,
	".ts": true, ".go": true, ".py": true, ".sh": true, ".c": true,
	".h": true, ".cpp": true, ".rs": true, ".java": true, ".ir": true,
}

var imageExt = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true,
}

var audioExt = map[string]bool{
	".wav": true, ".mp3": true, ".ogg": true, ".flac": true, ".m4a": true, ".aac": true, ".opus": true,
}

var videoExt = map[string]bool{
	".mp4": true, ".mkv": true, ".webm": true, ".avi": true, ".mov": true, ".m4v": true,
}

func IsText(name string) bool  { return textExt[strings.ToLower(filepath.Ext(name))] }
func IsImage(name string) bool { return imageExt[strings.ToLower(filepath.Ext(name))] }
func IsAudio(name string) bool { return audioExt[strings.ToLower(filepath.Ext(name))] }
func IsVideo(name string) bool { return videoExt[strings.ToLower(filepath.Ext(name))] }

func Join(root, rel string) (string, error) {
	rel = filepath.Clean(strings.TrimSpace(rel))
	if rel == "." || rel == "" {
		return root, nil
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes the CZHax working directory")
	}
	p := filepath.Join(root, rel)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	pAbs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if pAbs != rootAbs && !strings.HasPrefix(pAbs, rootAbs+string(os.PathSeparator)) {
		return "", errors.New("path escapes the CZHax working directory")
	}
	return pAbs, nil
}

func List(root, rel string) ([]Entry, error) {
	dir, err := Join(root, rel)
	if err != nil {
		return nil, err
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(items))
	for _, it := range items {
		info, err := it.Info()
		if err != nil {
			continue
		}
		name := it.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		r := filepath.Join(rel, name)
		out = append(out, Entry{
			Name:    name,
			Rel:     r,
			Path:    filepath.Join(dir, name),
			Dir:     it.IsDir(),
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime().Unix(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func ReadText(root, rel string) ([]string, error) {
	p, err := Join(root, rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if info.Size() > 2<<20 {
		return nil, errors.New("file is larger than 2 MB")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines, nil
}

func WriteText(root, rel string, lines []string) error {
	p, err := Join(root, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data := strings.Join(lines, "\n")
	if !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	return os.WriteFile(p, []byte(data), 0o644)
}

func CreateText(root, rel string) error {
	p, err := Join(root, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

func Mkdir(root, rel string) error {
	p, err := Join(root, rel)
	if err != nil {
		return err
	}
	return os.MkdirAll(p, 0o755)
}

func Delete(root, rel string) error {
	p, err := Join(root, rel)
	if err != nil {
		return err
	}
	if p == root {
		return errors.New("refusing to delete the working directory")
	}
	return os.RemoveAll(p)
}

func Rename(root, oldRel, newName string) (string, error) {
	oldPath, err := Join(root, oldRel)
	if err != nil {
		return "", err
	}
	newRel := filepath.Join(filepath.Dir(oldRel), newName)
	newPath, err := Join(root, newRel)
	if err != nil {
		return "", err
	}
	if newPath == oldPath {
		return newRel, nil
	}
	if _, err := os.Stat(newPath); err == nil {
		return "", fmt.Errorf("%s already exists", newName)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return "", err
	}
	return newRel, nil
}

func HumanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fK", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// ListAudio walks the working directory (two levels deep) for audio files.
func ListAudio(root string) ([]Entry, error) {
	var out []Entry
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if rel != "." && strings.Count(rel, string(os.PathSeparator)) >= 1 {
				return filepath.SkipDir
			}
			return nil
		}
		if !audioExt[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, Entry{
			Name:    d.Name(),
			Rel:     rel,
			Path:    path,
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime().Unix(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Rel) < strings.ToLower(out[j].Rel)
	})
	return out, nil
}
