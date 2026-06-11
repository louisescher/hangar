package present

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/louisescher/hangar/internal/doctor"
	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/lockfile"
	"github.com/louisescher/hangar/internal/security/audit"
)

func TestInstallReportJSON(t *testing.T) {
	log := audit.New(audit.OpUpdate)
	log.AddFinding(audit.TagRewriteFinding("pdf", "v1", "old", "new"))

	rep := install.Report{
		Skills: []install.SkillResult{
			{Name: "pdf", Kind: lockfile.KindSkill, FailedAgents: map[string]string{"zed": "boom"}},
			{Name: "readme", Kind: lockfile.KindRef},
		},
		InstalledAgents:      []string{"claude"},
		FailedAgents:         []string{"zed"},
		InstalledInstruction: "AGENTS.md",
		Audit:                log,
	}

	out := InstallReport(rep)
	if len(out.Skills) != 1 || out.Skills[0].Name != "pdf" {
		t.Errorf("skills = %+v", out.Skills)
	}
	if len(out.References) != 1 || out.References[0].Name != "readme" {
		t.Errorf("references = %+v", out.References)
	}
	if out.Skills[0].Failed["zed"] != "boom" {
		t.Errorf("failed map not carried: %+v", out.Skills[0])
	}
	if len(out.Findings) != 1 || out.Findings[0].Kind != "tag_rewritten" {
		t.Errorf("findings = %+v", out.Findings)
	}

	// Round-trips through encoding/json without error.
	var buf bytes.Buffer
	if err := WriteJSON(&buf, out); err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
}

func TestDoctorJSON(t *testing.T) {
	rep := doctor.Report{}
	out := Doctor(rep)
	if !out.Healthy {
		t.Error("empty report should be healthy")
	}
}
