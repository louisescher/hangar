// Package present renders Hangar command results as machine-readable JSON for
// scripting and agent consumption (the --json flag). The struct shapes here are
// the stable contract; plain-text output stays in the cmd layer.
package present

import (
	"encoding/json"
	"io"

	"github.com/louisescher/hangar/internal/doctor"
	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/lockfile"
)

// WriteJSON writes v as indented JSON followed by a newline.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ---- list -------------------------------------------------------------------

type ListSkill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
}

type ListResult struct {
	Source     string      `json:"source"`
	Ref        string      `json:"ref,omitempty"`
	Skills     []ListSkill `json:"skills"`
	References []string    `json:"references,omitempty"`
}

// List builds the JSON view of a discovery.
func List(d *engine.Discovered) ListResult {
	out := ListResult{Source: d.Source, Ref: d.Ref}
	for _, s := range d.Skills {
		out.Skills = append(out.Skills, ListSkill{Name: s.Name, Description: s.Description, Path: s.RelPath})
	}
	for _, r := range d.References {
		out.References = append(out.References, r.RelPath)
	}
	return out
}

// ---- install / update -------------------------------------------------------

type ResultItem struct {
	Name   string            `json:"name"`
	Kind   string            `json:"kind"`
	Failed map[string]string `json:"failed,omitempty"`
}

type Finding struct {
	Severity string `json:"severity"`
	Kind     string `json:"kind"`
	Skill    string `json:"skill,omitempty"`
}

type Report struct {
	Skills          []ResultItem `json:"skills"`
	References      []ResultItem `json:"references,omitempty"`
	InstalledAgents []string     `json:"installedAgents,omitempty"`
	FailedAgents    []string     `json:"failedAgents,omitempty"`
	Instruction     string       `json:"instruction,omitempty"`
	Findings        []Finding    `json:"findings,omitempty"`
}

// InstallReport builds the JSON view of an install/update report.
func InstallReport(rep install.Report) Report {
	out := Report{
		InstalledAgents: rep.InstalledAgents,
		FailedAgents:    rep.FailedAgents,
		Instruction:     rep.InstalledInstruction,
	}
	for _, sr := range rep.Skills {
		item := ResultItem{Name: sr.Name, Kind: sr.Kind, Failed: sr.FailedAgents}
		if sr.Kind == lockfile.KindRef {
			out.References = append(out.References, item)
		} else {
			out.Skills = append(out.Skills, item)
		}
	}
	if rep.Audit != nil {
		for _, f := range rep.Audit.Findings {
			out.Findings = append(out.Findings, Finding{Severity: string(f.Severity), Kind: string(f.Kind), Skill: f.Skill})
		}
	}
	return out
}

// ---- installed (manage / list) ---------------------------------------------

type InstalledEntry struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Source   string `json:"source"`
	Ref      string `json:"ref,omitempty"`
	Version  string `json:"version,omitempty"`
	Pinned   bool   `json:"pinned"`
	Outdated bool   `json:"outdated,omitempty"`
	Gone     bool   `json:"gone,omitempty"` // source/skill no longer exists upstream
	Latest   string `json:"latest,omitempty"`
}

// Installed builds the JSON view of installed lockfile entries. statuses may be
// nil (no update check performed).
func Installed(statuses []engine.InstalledStatus) []InstalledEntry {
	out := make([]InstalledEntry, 0, len(statuses))
	for _, st := range statuses {
		e := st.Entry
		out = append(out, InstalledEntry{
			Name: e.Name, Kind: e.Kind, Source: e.Source,
			Ref: e.Ref, Version: e.Version, Pinned: e.Pinned,
			Outdated: st.Outdated, Gone: st.Gone, Latest: st.Latest,
		})
	}
	return out
}

// ---- doctor -----------------------------------------------------------------

type Problem struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Fixable  bool   `json:"fixable"`
}

type DoctorResult struct {
	DetectedAgents []string  `json:"detectedAgents"`
	Healthy        bool      `json:"healthy"`
	Problems       []Problem `json:"problems"`
}

// Doctor builds the JSON view of a doctor report.
func Doctor(rep doctor.Report) DoctorResult {
	out := DoctorResult{Healthy: !rep.HasIssues()}
	for _, a := range rep.Detected {
		out.DetectedAgents = append(out.DetectedAgents, a.Def.Name)
	}
	for _, p := range rep.Problems {
		out.Problems = append(out.Problems, Problem{Severity: string(p.Severity), Message: p.Message, Fixable: p.Fixable})
	}
	return out
}
