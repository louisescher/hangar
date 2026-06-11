package tui

// skillSet tracks which skills are selected, keyed by their repo-relative path
// (unique per skill). It is shared between the tree and list views so toggling
// the view never loses a selection.
type skillSet struct {
	sel map[string]bool
}

func newSkillSet() *skillSet { return &skillSet{sel: map[string]bool{}} }

func (s *skillSet) has(key string) bool { return s.sel[key] }

func (s *skillSet) set(key string, on bool) {
	if on {
		s.sel[key] = true
	} else {
		delete(s.sel, key)
	}
}

func (s *skillSet) toggle(key string) { s.set(key, !s.has(key)) }

func (s *skillSet) count() int { return len(s.sel) }
