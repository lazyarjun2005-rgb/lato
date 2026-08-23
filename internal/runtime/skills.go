package runtime

import "lato/internal/skills"

// SkillCatalog returns the skill catalog discovered at startup: one
// entry per Markdown skill file in the user's skills directory. The
// catalog is what load_skill's ids refer to; full bodies stay on disk
// and are only loaded through the tool, never here.
func (r *Runtime) SkillCatalog() []skills.Skill {
	if r.skills == nil {
		return nil
	}
	return r.skills.Catalog()
}
