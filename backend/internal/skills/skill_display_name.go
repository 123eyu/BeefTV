package skills

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

// maxSkillNameRunes 是技能名称的长度上限，打包校验和显示名推断共用。
const maxSkillNameRunes = 80

// skillDisplayNamesRepairedKey 标记早先导入的技能已按中文标题修复过显示名；只修一次。
const skillDisplayNamesRepairedKey = "skills.display_names_repaired.v1"

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

// displaySkillName 用技能正文第一个一级标题作显示名，前提是包名是英文技术名、标题含中文且不超长；
// 代码块里的「#」行不算标题。其余情况保持原名。
func displaySkillName(name string, body []string) string {
	if !isTechnicalSkillName(name) {
		return name
	}
	fence := ""
	for _, line := range body {
		trimmed := strings.TrimSpace(line)
		if marker := codeFenceMarker(trimmed); marker != "" {
			if fence == "" {
				fence = marker
			} else if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if fence != "" || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") || !strings.HasPrefix(trimmed, "# ") {
			continue
		}
		title := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(strings.TrimPrefix(trimmed, "# ")), "#"))
		if strings.ContainsFunc(title, func(r rune) bool { return unicode.Is(unicode.Han, r) }) && utf8.RuneCountInString(title) <= maxSkillNameRunes {
			return title
		}
		return name
	}
	return name
}

func codeFenceMarker(line string) string {
	for _, marker := range []string{"```", "~~~"} {
		if strings.HasPrefix(line, marker) {
			return marker
		}
	}
	return ""
}

// ensureSkillDisplayNames 修复早先导入、仍显示英文技术名的技能，只执行一次；用户自己改过的名字、
// 包内容、版本和摘要都不动。内置技能的名字每次启动由预设重置，不在此修复。
func (s *Service) ensureSkillDisplayNames() error {
	if _, err := s.repo.SystemSetting(skillDisplayNamesRepairedKey); err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
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
	return s.repo.SaveSystemSetting(&model.SystemSetting{Key: skillDisplayNamesRepairedKey, ValueJSON: "true"})
}
