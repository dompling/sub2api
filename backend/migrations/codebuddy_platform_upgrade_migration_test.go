package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Checking only the final constraint misses upgrades that fail earlier while
// PostgreSQL validates existing CodeBuddy rows in an intermediate migration.
func TestCodeBuddyPlatformUpgradeChecksNeverNarrowAcceptedPlatforms(t *testing.T) {
	names, err := fs.Glob(FS, "*.sql")
	require.NoError(t, err)
	sort.Strings(names) // ApplyMigrations uses the same ordering.

	for _, tc := range []struct {
		name       string
		first      string
		constraint string
		column     string
	}{
		{"quotas", "233_user_platform_quotas_add_codebuddy.sql", "user_platform_quotas_platform_check", "platform"},
		{"routes", "234_composite_routes_add_codebuddy.sql", "composite_model_routes_target_platform_check", "target_platform"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := regexp.MustCompile(`CHECK \(` + tc.column + ` IN \(([^)]*)\)\)`)
			previous := []string{"codebuddy"}
			var last string
			for _, name := range names {
				if name < tc.first {
					continue
				}
				body, err := FS.ReadFile(name)
				require.NoError(t, err)
				sql := strings.Join(strings.Fields(string(body)), " ")
				if !strings.Contains(sql, "ADD CONSTRAINT "+tc.constraint) {
					continue
				}
				matches := check.FindAllStringSubmatch(sql, -1)
				require.NotEmpty(t, matches, "%s rebuilds %s without a platform CHECK", name, tc.constraint)
				for _, match := range matches {
					var current []string
					for _, raw := range strings.Split(match[1], ",") {
						current = append(current, strings.Trim(strings.TrimSpace(raw), "'"))
					}
					require.Subset(t, current, previous,
						"%s rejects platforms already accepted before it; existing rows would stop the upgrade", name)
					previous = current
				}
				last = name
			}
			require.Equal(t, "240_codebuddy_platform_constraints_superset.sql", last)
		})
	}
}

func TestDeployedCodeBuddyMigrationsRemainImmutable(t *testing.T) {
	for name, expected := range map[string]string{
		"232_api_key_allowed_models.sql":             "4449764385fecc18e89eab745fa2b2e6892befd4458d8968a1c4d54d39615837",
		"233_user_platform_quotas_add_codebuddy.sql": "4acdc184667b96d4f663f465731753043566d32f1ae1899ce4c9638651064ea5",
		"234_composite_routes_add_codebuddy.sql":     "640ef6bf8a589b99bf6f8e7d3bf2b9e9352ba4ff041c7aa691b36ff6f62b422c",
	} {
		t.Run(name, func(t *testing.T) {
			body, err := FS.ReadFile(name)
			require.NoError(t, err)
			sum := sha256.Sum256([]byte(strings.TrimSpace(string(body))))
			require.Equal(t, expected, hex.EncodeToString(sum[:]), "already deployed CodeBuddy migrations must retain their checksum")
		})
	}
}
