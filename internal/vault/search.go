package vault

import (
	"regexp"
	"strings"

	"gokeep/internal/kdbx"
)

// SearchOptions mirrors the options of the KeePass quick search and the "find" dialog.
type SearchOptions struct {
	// Query is the text (or regular expression) to look for.
	Query string `json:"query"`
	// GroupID restricts the search to one group and its subgroups.
	GroupID string `json:"groupId"`
	// SearchInAllGroups searches the whole database.
	SearchInAllGroups bool `json:"searchInAllGroups"`
	// CaseSensitive switches off the case insensitive comparison.
	CaseSensitive bool `json:"caseSensitive"`
	// Regex interprets Query as a regular expression.
	Regex bool `json:"regex"`
	// Fields selects which fields are searched; empty means all fields.
	Fields []string `json:"fields"`
	// IncludeRecycled includes entries inside the recycle bin.
	IncludeRecycled bool `json:"includeRecycled"`
	// IncludeProtected searches the password and other protected values as well.
	IncludeProtected bool `json:"includeProtected"`
}

// Search returns the entries matching the query.
func (v *Vault) Search(opts SearchOptions) ([]EntrySummary, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return nil, ErrLocked
	}
	if strings.TrimSpace(opts.Query) == "" {
		return v.collectSummaries(v.db.Root, true, opts.IncludeRecycled), nil
	}
	matcher, err := newMatcher(opts)
	if err != nil {
		return nil, err
	}
	var group *kdbx.Group
	if !opts.SearchInAllGroups {
		group, err = v.findGroupLocked(opts.GroupID)
		if err != nil {
			return nil, err
		}
		// Searching inside the recycle bin must include the deleted entries.
		if isInRecycleBin(v.db, group) {
			opts.IncludeRecycled = true
		}
	} else {
		group = v.db.Root
	}
	fields := map[string]bool{}
	for _, f := range opts.Fields {
		fields[f] = true
	}
	searchAll := len(fields) == 0

	var out []EntrySummary
	group.Walk(func(g *kdbx.Group) {
		if !opts.IncludeRecycled && isInRecycleBin(v.db, g) {
			return
		}
		for _, e := range g.Entries {
			if matcher.entryMatches(e, fields, searchAll, opts.IncludeProtected) {
				out = append(out, entryToSummary(v.db, e, g))
			}
		}
	}, func(*kdbx.Entry) {})
	return out, nil
}

// matcher applies the query to the fields of an entry.
type matcher struct {
	text   string
	re     *regexp.Regexp
	folded string
	fold   bool
	regex  bool
}

func newMatcher(opts SearchOptions) (*matcher, error) {
	m := &matcher{text: opts.Query, regex: opts.Regex, fold: !opts.CaseSensitive}
	if opts.Regex {
		pattern := opts.Query
		if !opts.CaseSensitive {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		m.re = re
		return m, nil
	}
	if !opts.CaseSensitive {
		m.folded = strings.ToLower(opts.Query)
	}
	return m, nil
}

func (m *matcher) matches(value string) bool {
	if m.regex {
		return m.re.MatchString(value)
	}
	if m.fold {
		return strings.Contains(strings.ToLower(value), m.folded)
	}
	return strings.Contains(value, m.text)
}

// entryMatches checks the standard fields, the tags and the custom fields.
func (m *matcher) entryMatches(e *kdbx.Entry, fields map[string]bool, all, includeProtected bool) bool {
	for _, f := range e.Strings {
		if !all && !fields[f.Key] {
			continue
		}
		if f.Protected && !includeProtected {
			continue
		}
		if m.matches(f.Value) {
			return true
		}
	}
	if all {
		for _, tag := range e.Tags {
			if m.matches(tag) {
				return true
			}
		}
		if e.OverrideURL != "" && m.matches(e.OverrideURL) {
			return true
		}
		for _, ref := range e.Binaries {
			if m.matches(ref.Key) {
				return true
			}
		}
	}
	return false
}

// FindDuplicates returns the groups of entries that share the same password.
func (v *Vault) FindDuplicates() ([][]EntrySummary, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return nil, ErrLocked
	}
	byPassword := map[string][]EntrySummary{}
	var order []string
	v.db.Root.Walk(func(g *kdbx.Group) {
		if isInRecycleBin(v.db, g) {
			return
		}
		for _, e := range g.Entries {
			password, _ := e.Field(kdbx.FieldPassword)
			if strings.TrimSpace(password) == "" {
				continue
			}
			if _, ok := byPassword[password]; !ok {
				order = append(order, password)
			}
			byPassword[password] = append(byPassword[password], entryToSummary(v.db, e, g))
		}
	}, func(*kdbx.Entry) {})
	var out [][]EntrySummary
	for _, key := range order {
		if len(byPassword[key]) > 1 {
			out = append(out, byPassword[key])
		}
	}
	return out, nil
}

// ExpiredEntries lists the entries whose expiry date has passed.
func (v *Vault) ExpiredEntries() ([]EntrySummary, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return nil, ErrLocked
	}
	var out []EntrySummary
	v.db.Root.Walk(func(g *kdbx.Group) {
		if isInRecycleBin(v.db, g) {
			return
		}
		for _, e := range g.Entries {
			if e.IsExpired(now()) {
				out = append(out, entryToSummary(v.db, e, g))
			}
		}
	}, func(*kdbx.Entry) {})
	return out, nil
}
