// Package taxonomy loads the file-backed categories, tags, links, menus, and users.
package taxonomy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/fengyuchen/mutiblog/internal/fsutil"
	"github.com/fengyuchen/mutiblog/internal/model"
)

type Store struct{ root string }
type Result struct {
	Categories map[string]*model.Category
	Tags       map[string]*model.Tag
	Links      map[string]*model.Link
	LinkGroups []*model.LinkGroup
	Menus      map[string]*model.Menu
	Users      map[string]*model.User
	Errors     []error
}

func NewStore(dataRoot string) *Store { return &Store{root: dataRoot} }
func (s *Store) LoadAll() Result {
	r := Result{
		Categories: map[string]*model.Category{},
		Tags:       map[string]*model.Tag{},
		Links:      map[string]*model.Link{},
		Menus:      map[string]*model.Menu{},
		Users:      map[string]*model.User{},
	}
	s.loadDir("categories", func(path, id string) error {
		var value model.Category
		if err := read(path, &value); err != nil {
			return err
		}
		if value.ID == "" {
			value.ID = id
		}
		if value.ID != id {
			return fmt.Errorf("category %s has mismatched id %s", path, value.ID)
		}
		r.Categories[id] = &value
		return nil
	}, &r)
	s.loadDir("tags", func(path, id string) error {
		var value model.Tag
		if err := read(path, &value); err != nil {
			return err
		}
		if value.ID == "" {
			value.ID = id
		}
		if value.ID != id {
			return fmt.Errorf("tag %s has mismatched id %s", path, value.ID)
		}
		r.Tags[id] = &value
		return nil
	}, &r)
	s.loadDir("links", func(path, id string) error {
		if id == "_groups" {
			var groups struct {
				Groups []*model.LinkGroup `yaml:"groups"`
			}
			if err := read(path, &groups); err != nil {
				return err
			}
			r.LinkGroups = groups.Groups
			return nil
		}
		var value model.Link
		if err := read(path, &value); err != nil {
			return err
		}
		if value.ID == "" {
			value.ID = id
		}
		r.Links[id] = &value
		return nil
	}, &r)
	s.loadDir("menus", func(path, id string) error {
		var value model.Menu
		if err := read(path, &value); err != nil {
			return err
		}
		if value.ID == "" {
			value.ID = id
		}
		r.Menus[id] = &value
		return nil
	}, &r)
	s.loadDir("users", func(path, id string) error {
		var value model.User
		if err := read(path, &value); err != nil {
			return err
		}
		if value.ID == "" {
			value.ID = id
		}
		r.Users[id] = &value
		return nil
	}, &r)
	r.Errors = append(r.Errors, ValidateCategories(r.Categories)...)
	sort.Slice(r.LinkGroups, func(i, j int) bool { return r.LinkGroups[i].Order < r.LinkGroups[j].Order })
	return r
}
func (s *Store) loadDir(name string, fn func(string, string) error, r *Result) {
	dir := filepath.Join(s.root, name)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		r.Errors = append(r.Errors, err)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".yaml")
		if err := fn(filepath.Join(dir, entry.Name()), id); err != nil {
			r.Errors = append(r.Errors, err)
		}
	}
}
func read(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, out)
}
func ValidateCategories(categories map[string]*model.Category) []error {
	var errs []error
	for id, c := range categories {
		if c.Parent != "" {
			if _, ok := categories[c.Parent]; !ok {
				errs = append(errs, fmt.Errorf("category %s has missing parent %s", id, c.Parent))
				c.Parent = ""
				continue
			}
			seen := map[string]bool{id: true}
			parent := c.Parent
			depth := 0
			for parent != "" {
				depth++
				if depth > 5 || seen[parent] {
					errs = append(errs, fmt.Errorf("category %s has cyclic or too-deep parent", id))
					c.Parent = ""
					break
				}
				seen[parent] = true
				next := categories[parent]
				if next == nil {
					break
				}
				parent = next.Parent
			}
		}
	}
	return errs
}
func (s *Store) SaveCategory(c *model.Category) error {
	if c == nil || c.ID == "" {
		return fmt.Errorf("category id required")
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(filepath.Join(s.root, "categories", c.ID+".yaml"), b, 0o644)
}
func (s *Store) SaveTag(value *model.Tag) error   { return s.save("tags", value.ID, value) }
func (s *Store) SaveLink(value *model.Link) error { return s.save("links", value.ID, value) }
func (s *Store) SaveMenu(value *model.Menu) error { return s.save("menus", value.ID, value) }
func (s *Store) SaveLinkGroups(groups []*model.LinkGroup) error {
	return s.save("links", "_groups", struct {
		Groups []*model.LinkGroup `yaml:"groups"`
	}{groups})
}
func (s *Store) Delete(kind, id string) error {
	if kind != "categories" && kind != "tags" && kind != "links" && kind != "menus" {
		return fmt.Errorf("unknown taxonomy %s", kind)
	}
	return os.Remove(filepath.Join(s.root, kind, id+".yaml"))
}
func (s *Store) save(kind, id string, value any) error {
	if id == "" {
		return fmt.Errorf("%s id required", kind)
	}
	b, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(filepath.Join(s.root, kind, id+".yaml"), b, 0o644)
}
