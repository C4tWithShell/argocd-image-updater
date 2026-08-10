package image

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/tag"
)

// The tests in this file demonstrate that no existing update strategy can
// correctly order CalVer tags of the form vYYYY-MM-DD[-REVISION].
//
// They are written against the *expected* (chronological) outcome, so they
// FAIL against the current implementation. That failure is the point: the
// output of `go test -run Test_CalVer ./pkg/image/` is the reproduction.

// calVerTagList builds a tag list where the tag date is deliberately identical
// for every tag, so that only the tag *name* carries version information. This
// mirrors registries that do not expose usable image creation timestamps.
func calVerTagList(tagNames []string) *tag.ImageTagList {
	tagList := tag.NewImageTagList()
	for _, tagName := range tagNames {
		tagList.Add(tag.NewImageTag(tagName, time.Unix(0, 0), ""))
	}
	return tagList
}

func Test_CalVer_SemVerStrategy(t *testing.T) {
	// Every CalVer tag parses as major=2026 with the date as a *prerelease*
	// identifier. Since the empty constraint defaults to "*", which excludes
	// prereleases, no tag is ever eligible and the image is never updated.
	t.Run("semver never selects any CalVer tag", func(t *testing.T) {
		tagList := calVerTagList([]string{"v2026-01-30", "v2026-09-01", "v2026-10-03"})
		img := NewFromIdentifier("example/app:v2026-01-30")
		vc := VersionConstraint{Strategy: StrategySemVer}

		newTag, err := img.GetNewestVersionFromTags(context.Background(), &vc, tagList)
		require.NoError(t, err)

		require.NotNil(t, newTag, "expected v2026-10-03 to be selected, but semver considered zero tags eligible")
		assert.Equal(t, "v2026-10-03", newTag.TagName)
	})
}

func Test_CalVer_AlphabeticalStrategy(t *testing.T) {
	// The alphabetical strategy is a raw byte comparison of the tag name, so
	// it is only correct when every numeric field is fixed width. Any variable
	// width field silently produces a wrong "newest" tag.
	tests := []struct {
		name     string
		tags     []string
		expected string
		why      string
	}{
		{
			name:     "zero padded dates sort correctly",
			tags:     []string{"v2026-01-30", "v2026-09-01", "v2026-10-03"},
			expected: "v2026-10-03",
			why:      "control case: fully padded tags are lexically ordered",
		},
		{
			name:     "unpadded month loses to an older date",
			tags:     []string{"v2026-1-30", "v2026-09-01"},
			expected: "v2026-09-01",
			why:      "'1-30' > '09-01' bytewise, so January is picked over September",
		},
		{
			name:     "unpadded revision loses to an older revision",
			tags:     []string{"v2026-01-30-2", "v2026-01-30-10"},
			expected: "v2026-01-30-10",
			why:      "'-2' > '-10' bytewise, so revision 2 is picked over revision 10",
		},
		{
			name:     "a stale unpadded tag pins the image forever",
			tags:     []string{"v2026-9-01", "v2026-10-03", "v2026-11-04", "v2026-12-25"},
			expected: "v2026-12-25",
			why:      "'9-01' outranks every two digit month, so one legacy tag blocks all future updates",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tagList := calVerTagList(tt.tags)
			img := NewFromIdentifier("example/app:" + tt.tags[0])
			vc := VersionConstraint{Strategy: StrategyAlphabetical}

			newTag, err := img.GetNewestVersionFromTags(context.Background(), &vc, tagList)
			require.NoError(t, err)
			require.NotNil(t, newTag)

			assert.Equal(t, tt.expected, newTag.TagName, tt.why)
		})
	}
}

func Test_CalVer_RevisionPaddingIsAmbiguous(t *testing.T) {
	// v2026-01-30-1 and v2026-01-30-01 denote the same revision but are two
	// distinct tags. No strategy can resolve which one is intended; the result
	// is decided by byte order alone.
	tagList := calVerTagList([]string{"v2026-01-30-1", "v2026-01-30-01"})
	img := NewFromIdentifier("example/app:v2026-01-30-1")
	vc := VersionConstraint{Strategy: StrategyAlphabetical}

	newTag, err := img.GetNewestVersionFromTags(context.Background(), &vc, tagList)
	require.NoError(t, err)
	require.NotNil(t, newTag)

	t.Logf("alphabetical picked %q out of %v (equivalent revisions, resolved by byte order)",
		newTag.TagName, tagList.Tags())
}

func Test_CalVer_SortOrderDump(t *testing.T) {
	// Prints the ordering each strategy produces next to the correct
	// chronological ordering, for inclusion in the issue report.
	tags := []string{
		"v2026-01-30", "v2026-01-30-01", "v2026-01-30-1", "v2026-01-30-2",
		"v2026-01-30-10", "v2026-1-30", "v2026-09-01", "v2026-10-03",
	}
	tagList := calVerTagList(tags)

	t.Logf("chronological: [v2026-01-30 v2026-01-30-1 v2026-01-30-2 v2026-01-30-10 v2026-09-01 v2026-10-03]")
	t.Logf("alphabetical:  %v", tagList.SortAlphabetically().Tags())
	t.Logf("semver:        %v", tagList.SortBySemVer(context.Background()).Tags())
}
