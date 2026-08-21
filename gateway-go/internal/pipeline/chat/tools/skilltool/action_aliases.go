package skilltool

import "strings"

// normalizeSkillManageAction maps Korean/underscore aliases onto skill_manage actions.
func normalizeSkillManageAction(raw string) string {
	a := strings.ToLower(strings.TrimSpace(raw))
	switch a {
	case "목록":
		return "list"
	case "생성":
		return "create"
	case "수정":
		return "patch"
	case "삭제":
		return "delete"
	case "읽기":
		return "read"
	case "list-files", "파일목록":
		return "list_files"
	case "write-file", "파일쓰기":
		return "write_file"
	case "remove-file", "파일삭제":
		return "remove_file"
	default:
		return a
	}
}
