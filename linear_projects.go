package main

import (
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"

	"kero/locales"
)

// A project of Shibu Code, a repository, is tracked in one Linear
// project: the one its Tasks pane shows and its agents look in. The
// user picks it from the project's menu, where the one named as the
// repository is offered first.

// projectLinearMenu lists the workspace's projects, the one the project
// is linked to checked and the one it is likely to be first, and None.
func (a *App) projectLinearMenu(m *ui.Menu, p proto.Project) {
	lin := a.st.Linear
	set := func(lp string) {
		a.call("project.linear", proto.ProjectLinearParams{Project: p.ID, LinearProject: lp}, nil)
	}
	if m.Item(locales.ProjectLinearNone()).Checked(p.LinearProject == "").Chosen() {
		set("")
	}
	m.Separator()
	guess := linearSuggest(p, lin.Projects)
	for _, lp := range linearProjectsFor(lin.Projects, guess) {
		label := lp.Name
		if lp.ID == guess && p.LinearProject == "" {
			label = locales.ProjectLinearSuggested(lp.Name)
		}
		if m.Item(label).Checked(p.LinearProject == lp.ID).Chosen() {
			set(lp.ID)
		}
	}
}

// linearProjectsFor are the projects in the menu's order: the guess
// first, then by name.
func linearProjectsFor(lps []proto.LinearProject, guess string) []proto.LinearProject {
	out := slices.Clone(lps)
	slices.SortStableFunc(out, func(a, b proto.LinearProject) int {
		switch {
		case a.ID == guess && b.ID != guess:
			return -1
		case b.ID == guess && a.ID != guess:
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
}

// linearSuggest is the id of the Linear project a project is likely
// tracked in: the one whose name is the repository's or the project's,
// letters and digits alone; "" when none is, or when two are.
func linearSuggest(p proto.Project, lps []proto.LinearProject) string {
	keys := []string{squash(p.Name)}
	if p.Dir != "" {
		keys = append(keys, squash(filepath.Base(p.Dir)))
	}
	pick := func(match func(name, key string) bool) string {
		found := ""
		for _, lp := range lps {
			n := squash(lp.Name)
			for _, k := range keys {
				if k != "" && n != "" && match(n, k) {
					if found != "" && found != lp.ID {
						return "-"
					}
					found = lp.ID
				}
			}
		}
		return found
	}
	if id := pick(func(n, k string) bool { return n == k }); id != "" {
		return strings.TrimPrefix(id, "-")
	}
	id := pick(func(n, k string) bool {
		return len(k) >= 4 && len(n) >= 4 && (strings.Contains(n, k) || strings.Contains(k, n))
	})
	return strings.TrimPrefix(id, "-")
}

// squash is a name's letters and digits, lowercase: "Shibu Code" and
// shibu-code are one.
func squash(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// projectLinear is the Linear project a project is linked to, nil none
// (or one the workspace no longer has).
func (a *App) projectLinear(projectID string) *proto.LinearProject {
	p := a.project(projectID)
	if p == nil || p.LinearProject == "" || a.st.Linear == nil {
		return nil
	}
	for i := range a.st.Linear.Projects {
		if a.st.Linear.Projects[i].ID == p.LinearProject {
			return &a.st.Linear.Projects[i]
		}
	}
	return nil
}
