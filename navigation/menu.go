// Copyright 2019 The Hugo Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package navigation

import (
	"fmt"
	"html/template"
	"sort"
	"strings"

	"github.com/pkg/errors"

	"github.com/gohugoio/hugo/common/maps"
	"github.com/gohugoio/hugo/common/types"
	"github.com/gohugoio/hugo/compare"

	"github.com/spf13/cast"
)

var smc = newMenuCache()

// MenuEntry represents a menu item defined in either Page front matter
// or in the site config.
type MenuEntry struct {
	// The URL value from front matter / config.
	ConfiguredURL string

	// The Page connected to this menu entry.
	Page Page

	// The path to the page, only relevant for menus defined in site config.
	PageRef string

	// The name of the menu entry.
	Name string

	// The menu containing this menu entry.
	Menu string

	// Used to identify this menu entry.
	Identifier string

	title string

	// If set, will be rendered before this menu entry.
	Pre template.HTML

	// If set, will be rendered after this menu entry.
	Post template.HTML

	// The weight of this menu entry, used for sorting.
	// Set to a non-zero value, negative or positive.
	Weight int

	// Identifier of the parent menu entry.
	Parent string

	// Child entries.
	Children Menu

	// User defined params.
	Params maps.Params
}

func (m *MenuEntry) URL() string {

	// Check page first.
	// In Hugo 0.86.0 we added `pageRef`,
	// a way to connect menu items in site config to pages.
	// This means that you now can have both a Page
	// and a configured URL.
	// Having the configured URL as a fallback if the Page isn't found
	// is obviously more useful, especially in multilingual sites.
	if !types.IsNil(m.Page) {
		return m.Page.RelPermalink()
	}

	return m.ConfiguredURL
}

// A narrow version of page.Page.
type Page interface {
	LinkTitle() string
	RelPermalink() string
	Path() string
	Section() string
	Weight() int
	IsPage() bool
	IsSection() bool
	IsAncestor(other any) (bool, error)
	Params() maps.Params
}

// Menu is a collection of menu entries.
type Menu []*MenuEntry

// Menus is a dictionary of menus.
type Menus map[string]Menu

// PageMenus is a dictionary of menus defined in the Pages.
type PageMenus map[string]*MenuEntry

// HasChildren returns whether this menu item has any children.
func (m *MenuEntry) HasChildren() bool {
	return m.Children != nil
}

// KeyName returns the key used to identify this menu entry.
func (m *MenuEntry) KeyName() string {
	if m.Identifier != "" {
		return m.Identifier
	}
	return m.Name
}

func (m *MenuEntry) hopefullyUniqueID() string {
	if m.Identifier != "" {
		return m.Identifier
	} else if m.URL() != "" {
		return m.URL()
	} else {
		return m.Name
	}
}

// IsEqual returns whether the two menu entries represents the same menu entry.
func (m *MenuEntry) IsEqual(inme *MenuEntry) bool {
	return m.hopefullyUniqueID() == inme.hopefullyUniqueID() && m.Parent == inme.Parent
}

// IsSameResource returns whether the two menu entries points to the same
// resource (URL).
func (m *MenuEntry) IsSameResource(inme *MenuEntry) bool {
	if m.isSamePage(inme.Page) {
		return m.Page == inme.Page
	}
	murl, inmeurl := m.URL(), inme.URL()
	return murl != "" && inmeurl != "" && murl == inmeurl
}

func (m *MenuEntry) isSamePage(p Page) bool {
	if !types.IsNil(m.Page) && !types.IsNil(p) {
		return m.Page == p
	}
	return false
}

// For internal use.
func (m *MenuEntry) MarshallMap(ime map[string]any) error {
	var err error
	for k, v := range ime {
		loki := strings.ToLower(k)
		switch loki {
		case "url":
			m.ConfiguredURL = cast.ToString(v)
		case "pageref":
			m.PageRef = cast.ToString(v)
		case "weight":
			m.Weight = cast.ToInt(v)
		case "name":
			m.Name = cast.ToString(v)
		case "title":
			m.title = cast.ToString(v)
		case "pre":
			m.Pre = template.HTML(cast.ToString(v))
		case "post":
			m.Post = template.HTML(cast.ToString(v))
		case "identifier":
			m.Identifier = cast.ToString(v)
		case "parent":
			m.Parent = cast.ToString(v)
		case "params":
			var ok bool
			m.Params, ok = maps.ToParamsAndPrepare(v)
			if !ok {
				err = fmt.Errorf("cannot convert %T to Params", v)
			}
		}
	}

	if err != nil {
		return errors.Wrapf(err, "failed to marshal menu entry %q", m.KeyName())
	}

	return nil
}

// This is for internal use only.
func (m Menu) Add(me *MenuEntry) Menu {
	m = append(m, me)
	// TODO(bep)
	m.Sort()
	return m
}

/*
 * Implementation of a custom sorter for Menu
 */

// A type to implement the sort interface for Menu
type menuSorter struct {
	menu Menu
	by   menuEntryBy
}

// Closure used in the Sort.Less method.
type menuEntryBy func(m1, m2 *MenuEntry) bool

func (by menuEntryBy) Sort(menu Menu) {
	ms := &menuSorter{
		menu: menu,
		by:   by, // The Sort method's receiver is the function (closure) that defines the sort order.
	}
	sort.Stable(ms)
}

var defaultMenuEntrySort = func(m1, m2 *MenuEntry) bool {
	if m1.Weight == m2.Weight {
		c := compare.Strings(m1.Name, m2.Name)
		if c == 0 {
			return m1.Identifier < m2.Identifier
		}
		return c < 0
	}

	if m2.Weight == 0 {
		return true
	}

	if m1.Weight == 0 {
		return false
	}

	return m1.Weight < m2.Weight
}

func (ms *menuSorter) Len() int      { return len(ms.menu) }
func (ms *menuSorter) Swap(i, j int) { ms.menu[i], ms.menu[j] = ms.menu[j], ms.menu[i] }

// Less is part of sort.Interface. It is implemented by calling the "by" closure in the sorter.
func (ms *menuSorter) Less(i, j int) bool { return ms.by(ms.menu[i], ms.menu[j]) }

// Sort sorts the menu by weight, name and then by identifier.
func (m Menu) Sort() Menu {
	menuEntryBy(defaultMenuEntrySort).Sort(m)
	return m
}

// Limit limits the returned menu to n entries.
func (m Menu) Limit(n int) Menu {
	if len(m) > n {
		return m[0:n]
	}
	return m
}

// ByWeight sorts the menu by the weight defined in the menu configuration.
func (m Menu) ByWeight() Menu {
	const key = "menuSort.ByWeight"
	menus, _ := smc.get(key, menuEntryBy(defaultMenuEntrySort).Sort, m)

	return menus
}

// ByName sorts the menu by the name defined in the menu configuration.
func (m Menu) ByName() Menu {
	const key = "menuSort.ByName"
	title := func(m1, m2 *MenuEntry) bool {
		return compare.LessStrings(m1.Name, m2.Name)
	}

	menus, _ := smc.get(key, menuEntryBy(title).Sort, m)

	return menus
}

// Reverse reverses the order of the menu entries.
func (m Menu) Reverse() Menu {
	const key = "menuSort.Reverse"
	reverseFunc := func(menu Menu) {
		for i, j := 0, len(menu)-1; i < j; i, j = i+1, j-1 {
			menu[i], menu[j] = menu[j], menu[i]
		}
	}
	menus, _ := smc.get(key, reverseFunc, m)

	return menus
}

// Clone clones the menu entries.
// This is for internal use only.
func (m Menu) Clone() Menu {
	return append(Menu(nil), m...)
}

func (m *MenuEntry) Title() string {
	if m.title != "" {
		return m.title
	}

	if m.Page != nil {
		return m.Page.LinkTitle()
	}

	return ""
}
