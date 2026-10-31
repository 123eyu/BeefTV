package skills

import (
	"fmt"
	"strings"
	"unicode"
)

// Technical names identify a package; a Chinese document title is its display name.
func isTechnicalSkillName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// displaySkillName 用技能正文第一个一级标题作显示名，前提是包名是英文技术名、标题含中文；
// 其余情况保持原名。
func displaySkillName(name string, body []string) string {
	if !isTechnicalSkillName(name) {
		return name
	}
	for _, line := range body {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "# ") {
			continue
		}
		if title := strings.TrimSpace(strings.TrimPrefix(trimmed, "# ")); strings.ContainsFunc(title, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			return title
		}
		return name
	}
	return name
}

// ensureSkillDisplayNames 修复早先导入、仍显示英文技术名的技能；用户自己改过的名字、包内容、版本和摘要都不动。
func (s *Service) ensureSkillDisplayNames() error {
	rows, err := s.repo.SkillsForDisplayNameEnsure()
	if err != nil {
		return err
	}
	for _, skill := range rows {
		metadata := parseSkillPackageMetadata([]byte(skill.Instruction))
		// Only repair the original technical name, preserving custom names and package bytes.
		if skill.Name != metadata.TechnicalName || metadata.Name == skill.Name || metadata.Name == "" {
			continue
		}
		skill.Name = metadata.Name
		if err := s.repo.SaveSkill(&skill); err != nil {
			return fmt.Errorf("更新技能显示名称失败: %w", err)
		}
	}
	return nil
}
