package indexer

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/entreya/nativestudio/knowledge"
)

var routePatterns = []*regexp.Regexp{regexp.MustCompile(`(?i)(?:app|router)\.(get|post|put|patch|delete)\s*\(\s*["']([^"']+)`), regexp.MustCompile(`HandleFunc\s*\(\s*["']([^"']+)`)}
var tablePattern = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?["'` + "`" + `]?([A-Za-z_][\w.]*)`)
var activeRecordPattern = regexp.MustCompile(`(?i)class\s+([A-Za-z_]\w*)\s+extends\s+(?:\\?[A-Za-z_\\]+)?ActiveRecord`)

func DetectFacts(file ScannedFile) []knowledge.Fact {
	source := knowledge.FactSource{Path: file.Path, StartLine: 1, EndLine: 1, ContentHash: file.Hash}
	makeFact := func(category, text string) knowledge.Fact {
		return knowledge.Fact{Category: category, Fact: text, Confidence: 1, Status: "verified", Sources: []knowledge.FactSource{source}}
	}
	base := strings.ToLower(filepath.Base(file.Path))
	var facts []knowledge.Fact
	switch base {
	case "go.mod":
		facts = append(facts, makeFact("build_system", "The project uses Go modules"))
	case "composer.json":
		facts = append(facts, makeFact("package_manager", "The project uses Composer"))
		lower := strings.ToLower(string(file.Content))
		if strings.Contains(lower, "yiisoft/yii2") {
			facts = append(facts, makeFact("framework", "The project uses Yii2"))
		}
		if strings.Contains(lower, "laravel/framework") {
			facts = append(facts, makeFact("framework", "The project uses Laravel"))
		}
	case "package.json":
		var manifest struct {
			Scripts         map[string]string `json:"scripts"`
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if json.Unmarshal(file.Content, &manifest) == nil {
			facts = append(facts, makeFact("package_manager", "The project uses an npm-compatible package manifest"))
			deps := map[string]string{}
			for key, value := range manifest.Dependencies {
				deps[key] = value
			}
			for key, value := range manifest.DevDependencies {
				deps[key] = value
			}
			for _, framework := range []string{"react", "vue", "svelte", "next", "vite", "typescript"} {
				if _, ok := deps[framework]; ok {
					facts = append(facts, makeFact("framework", fmt.Sprintf("The project uses %s", framework)))
				}
			}
			for name, command := range manifest.Scripts {
				if name == "build" || name == "test" || name == "dev" {
					facts = append(facts, makeFact("command", fmt.Sprintf("npm run %s executes %s", name, command)))
				}
			}
		}
	}
	normalized := strings.ToLower(filepath.ToSlash(file.Path))
	text := string(file.Content)
	at := func(category, value string, offset int) knowledge.Fact {
		line := 1 + strings.Count(text[:offset], "\n")
		return knowledge.Fact{Category: category, Fact: value, Confidence: 1, Status: "verified", Sources: []knowledge.FactSource{{Path: file.Path, StartLine: line, EndLine: line, ContentHash: file.Hash}}}
	}
	for _, pattern := range routePatterns {
		for _, match := range pattern.FindAllStringSubmatchIndex(text, -1) {
			method, pathIndex := "", 2
			if len(match) >= 6 {
				method = strings.ToUpper(text[match[2]:match[3]])
				pathIndex = 4
			}
			route := text[match[pathIndex]:match[pathIndex+1]]
			label := "The project registers route " + route
			if method != "" {
				label = "The project registers " + method + " route " + route
			}
			facts = append(facts, at("route", label, match[0]))
		}
	}
	for _, match := range tablePattern.FindAllStringSubmatchIndex(text, -1) {
		facts = append(facts, at("database_entity", "The database defines table "+text[match[2]:match[3]], match[0]))
	}
	for _, match := range activeRecordPattern.FindAllStringSubmatchIndex(text, -1) {
		facts = append(facts, at("database_entity", text[match[2]:match[3]]+" is an ActiveRecord model", match[0]))
	}
	if base == "main.go" || base == "index.php" || base == "main.py" {
		facts = append(facts, makeFact("entry_point", file.Path+" is a project entry point"))
	}
	if strings.Contains(normalized, "migration") {
		facts = append(facts, makeFact("database", "Database migrations are stored under "+filepath.ToSlash(filepath.Dir(file.Path))))
	}
	return facts
}
