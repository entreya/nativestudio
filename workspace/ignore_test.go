package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIgnoreMatcherExcludesDefaultDirectories(t *testing.T) {
	root := t.TempDir()
	matcher := NewIgnoreMatcher(root)
	cases := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"node_modules", true, true},
		{"node_modules/leftpad/index.js", false, true},
		{"vendor/composer/autoload.php", false, true},
		{".git/HEAD", false, true},
		{"src/main.go", false, false},
		{"src/services", true, false},
	}
	for _, c := range cases {
		if got := matcher.ShouldIgnore(c.path, c.isDir); got != c.want {
			t.Errorf("ShouldIgnore(%q, dir=%v) = %v, want %v", c.path, c.isDir, got, c.want)
		}
	}
}

func TestIgnoreMatcherRespectsRootGitignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\nruntime/\n!runtime/.gitkeep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	matcher := NewIgnoreMatcher(root)
	cases := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"app.log", false, true},
		{"logs/app.log", false, true},
		{"runtime", true, true},
		{"runtime/cache.php", false, true},
		{"runtime/.gitkeep", false, false},
		{"src/main.go", false, false},
	}
	for _, c := range cases {
		if got := matcher.ShouldIgnore(c.path, c.isDir); got != c.want {
			t.Errorf("ShouldIgnore(%q, dir=%v) = %v, want %v", c.path, c.isDir, got, c.want)
		}
	}
}

func TestIgnoreMatcherWithoutGitignoreOnlyAppliesDefaults(t *testing.T) {
	root := t.TempDir()
	matcher := NewIgnoreMatcher(root)
	if matcher.ShouldIgnore("README.md", false) {
		t.Fatal("expected a plain file with no .gitignore present to not be ignored")
	}
}

func TestSensitivePathPolicyBlocksCredentialShapedFiles(t *testing.T) {
	policy := NewSensitivePathPolicy()
	blocked := []string{
		".env", ".env.local", "config/id_rsa", "keys/id_ed25519",
		"secrets/credentials.json", "gcloud/service-account-prod.json",
		"app/secrets.yaml", ".npmrc", ".pypirc", ".netrc",
		"db/dump.sql", "backup.dump", "data.sqlite", "data.sqlite3",
		"certs/server.pem", "certs/server.key", "certs/client.p12", "certs/client.pfx",
	}
	for _, path := range blocked {
		if ok, reason := policy.IsSensitive(path); !ok || reason == "" {
			t.Errorf("expected %q to be reported sensitive with a reason, got ok=%v reason=%q", path, ok, reason)
		}
	}
}

func TestSensitivePathPolicyDoesNotBlockGenericNamesContainingSecretWords(t *testing.T) {
	policy := NewSensitivePathPolicy()
	allowed := []string{
		"app/services/SecretService.php",
		"src/secrets/README.md",
		"modules/keymanager/KeyController.go",
		"src/services/AuthService.php",
	}
	for _, path := range allowed {
		if ok, reason := policy.IsSensitive(path); ok {
			t.Errorf("expected %q to NOT be flagged sensitive, got reason=%q", path, reason)
		}
	}
}
