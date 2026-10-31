package agent

import _ "embed"

type skill struct {
	name        string
	description string
	content     string
}

//go:embed skills/gh-kit/SKILL.md
var kitSkill string

var bundled = []skill{{
	name:        "gh-kit",
	description: "Use gh kit for GitHub pull request comments, workflow cancellation, and CI job logs.",
	content:     kitSkill,
}}

func findSkill(name string) (skill, bool) {
	for _, s := range bundled {
		if s.name == name {
			return s, true
		}
	}
	return skill{}, false
}
