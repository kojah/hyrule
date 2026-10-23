package hyrule

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/kojah/hyrule/internal/gogen"
)

type ownedOutput struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type manifest struct {
	Version int                    `json:"version"`
	Targets map[string]ownedOutput `json:"targets"`
}
type publication struct {
	writes  map[string][]byte
	removes map[string]bool
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func (c *Config) readManifest() (manifest, []byte, error) {
	m := manifest{Version: 1, Targets: map[string]ownedOutput{}}
	data, err := os.ReadFile(filepath.Join(c.root, ".hyrule-manifest.json"))
	if os.IsNotExist(err) {
		return m, nil, nil
	}
	if err != nil {
		return m, nil, err
	}
	if err = json.Unmarshal(data, &m); err != nil {
		return m, nil, fmt.Errorf("manifest: %w", err)
	}
	if m.Version != 1 || m.Targets == nil {
		return m, nil, fmt.Errorf("invalid Hyrule manifest")
	}
	return m, data, nil
}

// Publish compares or writes validated artifacts and an ownership manifest.
// Check mode never writes. All safety checks precede writes. Each file is
// replaced atomically; the batch is not transactional. Obsolete outputs are
// removed only if their recorded hash still matches. Edited obsolete files
// cause an error. Callers must not publish concurrently to the same config.
func (c *Config) Publish(artifacts []Artifact, check bool) error {
	m, old, err := c.readManifest()
	if err != nil {
		return err
	}
	p := publication{map[string][]byte{}, map[string]bool{}}
	active := map[string]bool{}
	for _, t := range c.Targets {
		active[t.Name] = true
	}
	for name, entry := range m.Targets {
		if !active[name] {
			if err = c.obsolete(entry, p.removes); err != nil {
				return err
			}
			delete(m.Targets, name)
		}
	}
	for _, a := range artifacts {
		if err = c.planArtifact(a, &m, &p); err != nil {
			return err
		}
	}
	if err = c.checkCollisions(p); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(c.root, ".hyrule-manifest.json")
	if !bytes.Equal(old, data) {
		p.writes[path] = data
	}
	if check {
		return c.checkFresh(p)
	}
	return p.apply(path)
}

func (c *Config) planArtifact(a Artifact, m *manifest, p *publication) error {
	rel, err := filepath.Rel(c.root, a.Path)
	if err != nil || !filepath.IsLocal(rel) {
		return fmt.Errorf("artifact outside config directory: %s", a.Path)
	}
	valid := false
	for _, t := range c.Targets {
		if t.Name == a.Target && filepath.Join(c.root, t.Out) == a.Path {
			valid = true
		}
	}
	if !valid || !bytes.HasPrefix(a.Data, []byte(gogen.Header)) {
		return fmt.Errorf("invalid artifact %s", a.Path)
	}
	if previous, ok := m.Targets[a.Target]; ok && previous.Path != filepath.ToSlash(rel) {
		if err = c.obsolete(previous, p.removes); err != nil {
			return err
		}
	}
	if err = c.checkOutputPath(a.Path); err != nil {
		return err
	}
	current, err := os.ReadFile(a.Path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && !bytes.HasPrefix(current, []byte(gogen.Header)) {
		return fmt.Errorf("refusing to overwrite handwritten file %s", rel)
	}
	if !bytes.Equal(current, a.Data) {
		p.writes[a.Path] = a.Data
	}
	m.Targets[a.Target] = ownedOutput{filepath.ToSlash(rel), digest(a.Data)}
	return nil
}

func (c *Config) checkCollisions(p publication) error {
	for path := range p.removes {
		if _, ok := p.writes[path]; ok {
			return fmt.Errorf("obsolete output collides with new output: %s", path)
		}
		for _, t := range c.Targets {
			if filepath.Join(c.root, t.Out) == path {
				return fmt.Errorf("obsolete output still configured: %s", path)
			}
		}
	}
	return nil
}
func (c *Config) checkFresh(p publication) error {
	if len(p.writes) == 0 && len(p.removes) == 0 {
		return nil
	}
	paths := []string{}
	for path := range p.writes {
		rel, _ := filepath.Rel(c.root, path)
		paths = append(paths, rel)
	}
	for path := range p.removes {
		rel, _ := filepath.Rel(c.root, path)
		paths = append(paths, rel+" (obsolete)")
	}
	sort.Strings(paths)
	return fmt.Errorf("stale generated outputs: %v", paths)
}
func (p publication) apply(manifestPath string) error {
	paths := []string{}
	for path := range p.writes {
		if path != manifestPath {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := atomicWrite(path, p.writes[path]); err != nil {
			return err
		}
	}
	obsolete := []string{}
	for path := range p.removes {
		obsolete = append(obsolete, path)
	}
	sort.Strings(obsolete)
	for _, path := range obsolete {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if data, ok := p.writes[manifestPath]; ok {
		return atomicWrite(manifestPath, data)
	}
	return nil
}

func (c *Config) obsolete(entry ownedOutput, removes map[string]bool) error {
	if !filepath.IsLocal(entry.Path) || filepath.Ext(entry.Path) != ".go" {
		return fmt.Errorf("invalid owned path %s", entry.Path)
	}
	path := filepath.Join(c.root, entry.Path)
	if err := c.checkOutputPath(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(data, []byte(gogen.Header)) || digest(data) != entry.SHA256 {
		return fmt.Errorf("refusing to remove edited obsolete output %s", entry.Path)
	}
	removes[path] = true
	return nil
}

// Reject symlinks in every output component, including the final filename.
func (c *Config) checkOutputPath(path string) error {
	for path != c.root {
		rel, err := filepath.Rel(c.root, path)
		if err != nil || !filepath.IsLocal(rel) {
			return fmt.Errorf("output escapes config root")
		}
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing output symlink %s", path)
		}
		path = filepath.Dir(path)
	}
	return nil
}
func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".hyrule-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0644); err == nil {
		_, err = f.Write(data)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
