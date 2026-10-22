package main

import "slices"

// Each attached terminal keeps a copy of its session's scrollback, as
// large as kerod's (termOptions), so the app keeps no more than maxTerms
// of them: those of the session the user is in, and the ones shown most
// recently. A terminal dropped attaches again when its pane shows; kerod
// kept its session meanwhile.

// noteTerm counts a terminal just attached among those kept, as the last
// shown: every pane of the session on screen attaches, not only the one
// the user is in, and each must be dropped in its turn. It drops nothing,
// as it runs while a frame builds; touch does.
func (a *App) noteTerm(id string) {
	if !slices.Contains(a.lru, id) {
		a.lru = append(a.lru, id)
	}
}

// trimTerms drops the terminals shown longest ago past maxTerms, but
// never one of the session the user is in, whose panes show.
func (a *App) trimTerms() {
	shown := map[string]bool{}
	if s := a.session(a.selected); s != nil {
		for _, p := range s.Panes {
			shown[p.ID] = true
		}
	}
	for i := 0; len(a.lru) > maxTerms && i < len(a.lru); {
		if id := a.lru[i]; !shown[id] {
			a.dropTerm(id) // removes it from a.lru
			continue
		}
		i++
	}
}
