package skills

import (
	"strings"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestImportedSkillUsesChineseHeadingInsteadOfTechnicalName(t *testing.T) {
	s, db := agentSkillFixture(t)
	body := "---\nname: storyboard-text\ndescription: 整理分镜\n---\n# 双镜头文字分镜\n\n正文"
	pin := installAgentSkill(t, s, map[string]string{"SKILL.md": body})
	item, err := s.SkillDetail("owner", pin.SkillID)
	if err != nil || item.SkillName != "双镜头文字分镜" {
		t.Fatalf("imported name: %+v %v", item, err)
	}
	if err := db.Model(&model.Skill{}).Where("id = ?", pin.SkillID).Update("name", "storyboard-text").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSkillPackages(); err != nil {
		t.Fatal(err)
	}
	item, err = s.SkillDetail("owner", pin.SkillID)
	if err != nil || item.SkillName != "双镜头文字分镜" || item.VersionID != pin.VersionID || item.ContentHash != pin.ContentHash {
		t.Fatalf("legacy name repair changed identity: %+v %v", item, err)
	}
	resource, err := s.AgentVersion("owner", pin)
	if err != nil || resource.Name != item.SkillName || resource.Instruction != body {
		t.Fatalf("assistant label/body: %+v %v", resource, err)
	}
	if err := db.Model(&model.Skill{}).Where("id = ?", pin.SkillID).Update("name", "我的私人分镜").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSkillPackages(); err != nil {
		t.Fatal(err)
	}
	item, err = s.SkillDetail("owner", pin.SkillID)
	if err != nil || item.SkillName != "我的私人分镜" {
		t.Fatalf("custom name replaced: %+v %v", item, err)
	}
}

func TestSkillDisplayNameFallbacks(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"---\nname: interview-outline\ndescription: 简介\n---\n# 采访提纲", "采访提纲"},
		{"---\nname: material-inventory\ndescription: 简介\n---\n# 素材清单整理", "素材清单整理"},
		{"---\nname: Custom Name\ndescription: 简介\n---\n# 中文标题", "Custom Name"},
		{"---\nname: 自定义名称\ndescription: 简介\n---\n# 中文标题", "自定义名称"},
		{"---\nname: technical-name\ndescription: 简介\n---\nNo title", "technical-name"},
		// 代码块里的注释不是标题；ATX 结尾的 # 不进名字；超长标题不采用，避免导入因名称超长失败。
		{"---\nname: setup-guide\ndescription: 简介\n---\n```bash\n# 安装依赖\n```\n# 环境准备指南", "环境准备指南"},
		{"---\nname: tilde-fence\ndescription: 简介\n---\n~~~\n# 注释\n~~~\n# 波浪线代码块之后的标题", "波浪线代码块之后的标题"},
		{"---\nname: closing-hash\ndescription: 简介\n---\n# 分镜整理 ##", "分镜整理"},
		{"---\nname: long-title\ndescription: 简介\n---\n# " + strings.Repeat("长", maxSkillNameRunes+1), "long-title"},
	} {
		if got := parseSkillPackageMetadata([]byte(tc.body)).Name; got != tc.want {
			t.Fatalf("name = %q, want %q", got, tc.want)
		}
	}
}

// 早先导入、来源字段为空（NULL）的技能也会被修复；修复只执行一次，之后用户主动改回技术名不会被再次改掉。
func TestSkillDisplayNameRepairCoversNullSourceAndRunsOnce(t *testing.T) {
	s, db := agentSkillFixture(t)
	pin := installAgentSkill(t, s, map[string]string{"SKILL.md": "---\nname: interview-outline\ndescription: 采访\n---\n# 采访提纲\n\n正文"})
	if err := db.Exec("UPDATE skills SET name = ?, source_type = NULL WHERE id = ?", "interview-outline", pin.SkillID).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSkillPackages(); err != nil {
		t.Fatal(err)
	}
	item, err := s.SkillDetail("owner", pin.SkillID)
	if err != nil || item.SkillName != "采访提纲" || item.ContentHash != pin.ContentHash {
		t.Fatalf("NULL-source legacy name not repaired: %+v %v", item, err)
	}
	if err := db.Model(&model.Skill{}).Where("id = ?", pin.SkillID).Update("name", "interview-outline").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSkillPackages(); err != nil {
		t.Fatal(err)
	}
	if item, err = s.SkillDetail("owner", pin.SkillID); err != nil || item.SkillName != "interview-outline" {
		t.Fatalf("repair ran again over the user's chosen name: %+v %v", item, err)
	}
}
